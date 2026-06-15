# Kafka Topic Operator — 프로젝트 계획 문서

## 1. 프로젝트 소개

### **Kafka Topic Operator**

Kafka 클러스터의 토픽을 CRD/CR 기반으로 선언적 관리(생성·수정·삭제)하는 오퍼레이터.

---

## 2. 선정 배경, 목표

### Operator 도입으로 해결할 수 있는 부분이 많다

- **명령형 방식의 문제점**
    - **명령형 방식**: `kafka-topics.sh --create ...` 같은 일회성 명령을 직접 실행해 토픽을 만들고 수정하는 방식
    - 누가 만들었는지 추적 불가
    - 누군가 바꾼 설정(드리프트)으로 인한 장애
    - 환경 간 설정을 일치시키기 어려움
    - 대량 토픽 생성 시 누락

- **Operator(선언적 관리) 도입시 얻는 효과**
    - **목표 상태가 명시적으로 기록됨**: 토픽이 어떤 모양이어야 하는지(파티션 수, retention 등)가 `KafkaTopic` 오브젝트(YAML)로 남는다.
    - **GitOps와 연동 가능**: 토픽이 k8s 오브젝트가 되면, 다른 k8s 리소스처럼 매니페스트를 Git에 두고 ArgoCD로 동기화할 수 있다.
    - **멱등성(Idempotency)**: 같은 매니페스트를 몇 번 적용해도 결과는 항상 같은 상태로 수렴한다.
    - **드리프트 자동 교정(Self-healing)**: 누군가 운영 중에 토픽 설정을 수동으로 바꿔도, Operator가 주기적으로 비교해 선언된 상태로 되돌린다.
    - **재현성**: 새 클러스터를 띄울 때 매니페스트만 적용하면 토픽이 동일하게 재구성된다. 환경(dev/staging/prod) 간 설정 불일치가 사라진다.

### 관련 기능을 제공하는 솔루션

- **Amazon MSK**: Amazon의 관리형 Kafka 서비스. 2026년 2월 선언적 topic 관리 API가 도입되었다. 단, MSK(AWS)에 종속적이다. [MSK simplifies Kafka topic management with new APIs and console integration
](https://aws.amazon.com/about-aws/whats-new/2026/02/amazon-msk-kafka-topics-public-apis/)

- **Strimzi**: k8s 환경에서의 Kafka 클러스터 배포 및 관리를 위한 오픈소스 Operator. 내부에 topic operator도 있다. [Strimzi operator 깃헙 레포](https://github.com/strimzi/strimzi-kafka-operator)


### 목표

이미 Kafka 토픽을 선언적으로 관리하는 성숙한 솔루션들이 존재하기 때문에 새로운 기능을 만드는 것은 아니다. 이번 프로젝트에서는 Java로 구현된 Strimzi의 Topic Operator 핵심 기능을 CNCF 표준인 Go, Kubebuilder로 직접 재구현해 보며 k8s 커스텀 컨트롤러의 동작 원리를 깊이 있게 이해하는 것이 목표이다.

### 제한 범위

- Kafka 클러스터는 k8s 클러스터 외부에 있다고 가정한다.
- 스터디 k8s 클러스터는 5개 팀이 공유한다 (`rule.md`). 우리는 2팀으로, 모든 실습 리소스는 namespace `team-2`에서만 운영한다.
  - Operator Deployment, `KafkaTopic` CR, ConfigMap/Secret 모두 `team-2`에 둔다.
  - `KafkaTopic` CRD는 본질적으로 cluster-scoped 이지만 controller의 watch 범위는 `team-2`로 제한한다 (다른 팀이 같은 CRD를 사용해도 우리 operator는 반응하지 않음).

---

## 3. Operator가 관리할 리소스와 CRD

### 실제 관리 대상
- 외부 Kafka의 토픽, 파티션 개수, 레플리카 개수, config
- 정의할 CRD는 `KafkaTopic` 하나이다.

### Kind / API Group

| 항목 | 값 |
|---|---|
| Kind | `KafkaTopic` |
| API Group | `kafka.study.dev` |
| Version | `v1alpha1` |
| Full apiVersion | `kafka.study.dev/v1alpha1` |

> 공유 클러스터 규칙(`rule.md`): API Group은 팀별로 겹치지 않게 사용한다. 우리 팀(2팀)은 `kafka.study.dev` 사용.

### Spec 핵심 필드

| 필드 | 타입 | 설명 |
|---|---|---|
| `topicName` | string | 실제 Kafka 토픽 이름 |
| `partitions` | int32 | 파티션 수 (증가만 가능, 감소 불가) |
| `replicationFactor` | int16 | 브로커 간 복제 수 |
| `config` | map[string]string | Kafka 토픽 설정 (retention, cleanup 등) |

### Status 핵심 필드

| 필드 | 타입 | 설명 |
|---|---|---|
| `conditions` | []metav1.Condition | 표준 Conditions (Ready, ConfigDrifted) |
| `observedPartitions` | int32 | Kafka에서 관찰된 실제 파티션 수 |
| `observedGeneration` | int64 | 처리 완료된 spec 버전 |

### Conditions 설계

| Type | True 의미 | Reason 예시 |
|---|---|---|
| `Ready` | 토픽이 정상 동기화됨 | `TopicSynced`, `KafkaUnreachable`, `PartitionDecreaseNotAllowed`, `Reconciling` |
| `ConfigDrifted` | spec과 실제 Kafka 설정이 불일치 | `ConfigUpdated`, `DriftDetected` |

### 코드 예시

```go
// KafkaTopicSpec defines the desired state of KafkaTopic
type KafkaTopicSpec struct {
    // TopicName은 실제 카프카에 생성될 토픽 이름
    TopicName string `json:"topicName"`
    
    // Partitions 파티션 개수
    Partitions int32 `json:"partitions"`
    
    // ReplicationFactor 레플리카 개수
    ReplicationFactor int16 `json:"replicationFactor"`
    
    // Config 토픽별 커스텀 설정 (예: retention.ms)
    // +optional
    Config map[string]string `json:"config,omitempty"`
}

// KafkaTopicStatus defines the observed state of KafkaTopic
type KafkaTopicStatus struct {
    //+optional
    Conditions []metav1.Condition `json:"conditions,omitempty"`
    //+optional
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`
    //+optional
    ObservedPartitions int32 `json:"observedPartitions,omitempty"`
}
```

---

## 4. 2주 진행 계획

### 1주차: 기본 동작 완성

**목표: KafkaTopic CR을 만들면 Kafka에 토픽이 생성되고, Status에 결과가 반영되는 것까지.**

|   | 작업 내용 |
|---|---|
| 1 | 환경 세팅 + Kafka Admin API 탐색 (docker-compose + kafka-go 접속 테스트) |
| 2 | 프로젝트 스캐폴딩 + CRD 타입 정의 (`kafkatopic_types.go` 작성) |
| 3 | Kafka 클라이언트 모듈 작성 (`internal/kafka/client.go` — CreateTopic / DescribeTopic / DeleteTopic / UpdateConfig) |
| 4 | 기본 Reconcile 구현 (CR 생성 → Kafka 토픽 생성, 연결 실패 시 RequeueAfter 30초) |
| 5 | Status + Conditions 구현 (Ready, ObservedGeneration, Printer Columns) |

---

### 2주차: Drift 감지 + Finalizer + 마무리

**목표: 설정 변경 자동 반영, 삭제 시 Kafka 토픽도 정리**

|    | 작업 내용 |
|----|---|
| 6  | Drift 감지 + 자동 수정 (spec.config vs 실제 config 비교, ConfigDrifted Condition) |
| 7  | Partition 증가 처리 + 감소 불가 케이스 (PartitionDecreaseNotAllowed) |
| 8  | Finalizer 구현 (CR 삭제 시 Kafka 토픽 삭제, 멱등 처리) |
| 9  | 통합 테스트 + 에지 케이스 (연결 실패, 동시 다수 CR 생성 등) |
| 10 | README 작성 + 시연 시나리오 + 발표 자료 정리 |

---
# Kafka Topic Operator — 역할 분담

## 분담 기준

```
담당자 1: Kafka 클라이언트 레이어
  파일: internal/kafka/client.go
  역할: Kafka Admin API 호출, Kafka 도메인 로직

담당자 2: K8s 오퍼레이터 레이어
  파일: internal/controller/kafkatopic_controller.go
  역할: Reconcile, Status, Finalizer, CRD 정의
```

### 인터페이스 먼저 합의

두 담당자가 아래 인터페이스를 먼저 합의하면 독립적으로 작업 가능.

```go
type KafkaClient interface {
    DescribeTopic(name string) (*TopicInfo, error)
    CreateTopic(name string, partitions int, replication int16, config map[string]string) error
    DeleteTopic(name string) error
    UpdateConfig(name string, config map[string]string) error
}
```

담당자 1이 이 인터페이스를 구현하고,
담당자 2는 mock으로 Reconcile 먼저 개발 → 나중에 통합.

---

## 1주차

### 담당자 1 — Kafka 클라이언트

**목표: Kafka Admin API를 Go로 다룰 수 있는 클라이언트 모듈 완성**

진행 순서

1. 로컬 환경 세팅
    - docker-compose로 Kafka 실행
    - kafka-go 라이브러리로 접속 테스트 스크립트 작성

2. `internal/kafka/client.go` 구현
    - `DescribeTopic` — 토픽 존재 여부 + 현재 설정 조회
    - `CreateTopic` — 토픽 생성
    - `DeleteTopic` — 토픽 삭제
    - `UpdateConfig` — 토픽 설정 변경

3. 클라이언트 단독 테스트
    - 오퍼레이터 없이 Go 스크립트로 검증
    - 토픽 생성 → 조회 → 설정 변경 → 삭제 흐름 확인

---

### 담당자 2 — K8s 오퍼레이터

**목표: KafkaTopic CR을 만들면 Kafka에 토픽이 생성되고 Status에 반영되는 것까지**

진행 순서

1. 프로젝트 스캐폴딩
   ```bash
   kubebuilder init \
     --domain study.dev \
     --repo github.com/cloud-club/09th-k8s-crd-operator/projects/kafka-operator

   kubebuilder create api \
     --group kafka \
     --version v1alpha1 \
     --kind KafkaTopic
   ```
   → API Group은 `kafka.study.dev` 가 된다 (`--domain` + `--group`).

2. CRD 타입 정의 (`api/v1alpha1/kafkatopic_types.go`)
    - Spec 필드 정의 (bootstrapServers, topicName, partitions 등)
    - Status 필드 정의 (conditions, observedPartitions, observedGeneration)
    - Kubebuilder 마커 작성
    - `make manifests` 실행

3. 기본 Reconcile 구현 (`internal/controller/kafkatopic_controller.go`)
    - KafkaClient 인터페이스 mock으로 Reconcile 로직 작성
    - CR 생성 → 토픽 생성 흐름
    - Kafka 연결 실패 → `RequeueAfter` 30초

4. Status + Conditions 구현
    - `Ready` Condition (TopicSynced / KafkaUnreachable)
    - `ObservedGeneration` 반영
    - Printer Columns 설정

5. 담당자 1 클라이언트와 통합
    - mock → 실제 `kafka.NewClient()` 교체
    - `make run`으로 통합 테스트

---

## 2주차

### 담당자 1 — Kafka 클라이언트 확장

**목표: Drift 감지 및 Partition 변경 처리 로직 완성**

진행 순서

1. Drift 감지 로직
    - `DescribeTopic` 결과와 spec 비교 함수 작성
    - config map 비교 (key/value 단위)
    - 변경된 항목만 추출

2. Partition 처리
    - Partition 증가 함수 (`AddPartitions`)
    - Partition 감소 시도 시 `PartitionDecreaseError` 반환

3. 에러 타입 정의
   ```go
   type TopicNotFoundError struct{ TopicName string }
   type PartitionDecreaseError struct {
       Current int32
       Desired int32
   }
   type KafkaUnreachableError struct{ Err error }
   ```

4. 클라이언트 단독 테스트 보완
    - Partition 증가 시나리오 검증
    - Partition 감소 시도 시 에러 반환 확인
    - config 변경 drift 감지 확인

---

### 담당자 2 — 오퍼레이터 고도화 + 배포

**목표: Drift 감지 + Finalizer 구현, 가비아 클러스터 배포**

진행 순서

1. Drift 감지 Reconcile 통합
    - 담당자 1의 Drift 감지 로직을 Reconcile에 연결
    - `ConfigDrifted` Condition 업데이트
    - config 변경 시 자동 반영

2. Partition 변경 Reconcile 통합
    - Partition 증가 → Kafka에 반영
    - Partition 감소 → `Ready=False`, `PartitionDecreaseNotAllowed` Condition

3. Finalizer 구현
    - CR 생성 시 Finalizer 추가
    - CR 삭제 시 Kafka 토픽 삭제 → Finalizer 제거
    - 이미 없는 토픽 삭제 → 성공 처리 (멱등)
    - Kafka 연결 실패 시 → Finalizer 유지, 재시도

4. 가비아 클러스터 배포
    - 이미지 빌드 + 푸시
    - `make deploy`로 배포
    - 가비아 클러스터에서 동작 확인

5. 발표 준비
    - README 작성
    - 시연 시나리오 스크립트
    - 발표 자료 정리
