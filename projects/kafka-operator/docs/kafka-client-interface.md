# KafkaClient 인터페이스 설계 합의서

> 담당자 1(Kafka 클라이언트)과 담당자 2(K8s 오퍼레이터)가 독립적으로 개발하기 위한 사전 합의 문서.
> 스캐폴딩 이후 `internal/kafka/interface.go`가 source of truth가 되고, 이 문서는 rationale로 남는다.

---

## 1. 설계 원칙

1. **멱등성은 controller가 책임진다.** 클라이언트는 "이미 존재함", "없음"을 **typed error**로 알리고, "다시 시도", "Ready=False" 같은 정책 판단은 Reconcile에서 결정한다.
2. **입력은 struct로 받는다.** 필드가 늘어도 시그니처가 안 깨진다.
3. **모든 메서드는 `context.Context`를 받는다.** Reconcile은 항상 ctx를 보유하며, 클라이언트는 이를 Kafka admin API로 전달해 취소/타임아웃을 지원한다.
4. **에러는 sentinel + typed.** `errors.Is(err, ErrTopicNotFound)` 로 분기 가능하게.
5. **2주차 메서드 시그니처도 미리 포함**해서 인터페이스 break를 줄인다.

---

## 2. 인터페이스 정의

```go
package kafka

import "context"

// KafkaClient는 Kafka Admin API에 대한 도메인 추상화.
// 멱등성/재시도는 호출자(controller)가 결정한다.
type KafkaClient interface {
    // DescribeTopic은 토픽의 현재 상태를 조회한다.
    // 토픽이 없으면 ErrTopicNotFound 를 반환한다.
    DescribeTopic(ctx context.Context, name string) (*TopicInfo, error)

    // CreateTopic은 새 토픽을 생성한다.
    // 이미 존재하면 ErrTopicAlreadyExists 를 반환한다.
    CreateTopic(ctx context.Context, spec TopicSpec) error

    // DeleteTopic은 토픽을 삭제한다.
    // 토픽이 없으면 ErrTopicNotFound 를 반환한다 (호출자가 멱등 처리).
    DeleteTopic(ctx context.Context, name string) error

    // UpdateConfig는 토픽의 동적 설정(retention.ms 등)만 변경한다.
    // 파티션/레플리카는 변경하지 않는다. 토픽이 없으면 ErrTopicNotFound.
    UpdateConfig(ctx context.Context, name string, config map[string]string) error

    // AddPartitions는 토픽의 총 파티션 수를 total로 늘린다 (2주차에 구현).
    // 현재보다 작으면 ErrPartitionDecrease 를 반환한다.
    // 현재와 같으면 nil (noop) 을 반환한다.
    AddPartitions(ctx context.Context, name string, total int32) error
}
```

---

## 3. 타입 정의

```go
// TopicInfo는 DescribeTopic의 반환값.
type TopicInfo struct {
    Name              string
    Partitions        int32
    ReplicationFactor int16
    Config            map[string]string
}

// TopicSpec은 CreateTopic의 입력.
type TopicSpec struct {
    Name              string
    Partitions        int32
    ReplicationFactor int16
    Config            map[string]string
}
```

---

## 4. 에러 타입

`errors.Is` 로 식별 가능한 sentinel 에러로 통일한다.

```go
import "errors"

var (
    // ErrTopicNotFound: 대상 토픽이 Kafka에 존재하지 않음.
    ErrTopicNotFound = errors.New("kafka: topic not found")

    // ErrTopicAlreadyExists: 같은 이름의 토픽이 이미 존재함.
    ErrTopicAlreadyExists = errors.New("kafka: topic already exists")

    // ErrKafkaUnreachable: 브로커 연결 실패 (네트워크/인증 등).
    // controller는 RequeueAfter 로 재시도.
    ErrKafkaUnreachable = errors.New("kafka: broker unreachable")

    // ErrPartitionDecrease: 파티션 감소 시도 (Kafka는 감소 불가).
    // 추가 컨텍스트가 필요하면 typed error 로 wrap.
    ErrPartitionDecrease = errors.New("kafka: partition decrease not allowed")
)

// 부가 정보가 필요한 경우는 wrapping 한다.
type PartitionDecreaseError struct {
    Current int32
    Desired int32
}

func (e *PartitionDecreaseError) Error() string { /* ... */ }
func (e *PartitionDecreaseError) Unwrap() error { return ErrPartitionDecrease }
```

> 호출자는 `errors.Is(err, kafka.ErrTopicNotFound)` 로 분기하고,
> 추가 정보가 필요하면 `var pe *kafka.PartitionDecreaseError; errors.As(err, &pe)` 로 꺼낸다.

---

## 5. 메서드별 동작 계약

### `DescribeTopic(ctx, name) (*TopicInfo, error)`

| 케이스 | 반환 |
|---|---|
| 토픽 존재 | `(*TopicInfo, nil)` — Config은 **동적 설정(override된 값 + 기본값)** 전체를 반환 |
| 토픽 없음 | `(nil, ErrTopicNotFound)` |
| 브로커 연결 실패 | `(nil, ErrKafkaUnreachable)` (wrap 가능) |

- `Config` 맵에는 Kafka가 반환하는 **모든 config key**가 포함된다. Controller는 spec.config에 명시된 key만 비교한다 (drift 판단을 위해).

### `CreateTopic(ctx, spec) error`

| 케이스 | 반환 |
|---|---|
| 생성 성공 | `nil` |
| 이미 존재 | `ErrTopicAlreadyExists` |
| 잘못된 파라미터 (partitions ≤ 0 등) | 일반 error (validation은 CRD에서도 처리) |
| 브로커 연결 실패 | `ErrKafkaUnreachable` |

### `DeleteTopic(ctx, name) error`

| 케이스 | 반환 |
|---|---|
| 삭제 성공 | `nil` |
| 토픽 없음 | `ErrTopicNotFound` (호출자가 nil로 변환해서 멱등 처리) |
| 브로커 연결 실패 | `ErrKafkaUnreachable` |

### `UpdateConfig(ctx, name, config) error`

| 케이스 | 반환 |
|---|---|
| 적용 성공 | `nil` |
| 토픽 없음 | `ErrTopicNotFound` |
| 알 수 없는 config key | 일반 error |
| 브로커 연결 실패 | `ErrKafkaUnreachable` |

- 전달된 config는 **명시된 key만 override** 한다 (전체 덮어쓰기 X). Kafka admin AlterConfigs `INCREMENTAL` 모드 사용.

### `AddPartitions(ctx, name, total) error` (2주차)

| 케이스 | 반환 |
|---|---|
| `total == current` | `nil` (noop) |
| `total > current` | `nil` (증가) |
| `total < current` | `*PartitionDecreaseError{Current, Desired}` (Unwrap → `ErrPartitionDecrease`) |
| 토픽 없음 | `ErrTopicNotFound` |

---

## 6. 호출 패턴 예시 (Reconcile 측)

```go
func (r *KafkaTopicReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var kt kafkav1alpha1.KafkaTopic
    if err := r.Get(ctx, req.NamespacedName, &kt); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    info, err := r.Kafka.DescribeTopic(ctx, kt.Spec.TopicName)
    switch {
    case errors.Is(err, kafka.ErrTopicNotFound):
        // 생성 흐름으로
        if err := r.Kafka.CreateTopic(ctx, toSpec(&kt)); err != nil {
            return r.handleKafkaErr(ctx, &kt, err)
        }
        return r.markReady(ctx, &kt, "TopicCreated")

    case errors.Is(err, kafka.ErrKafkaUnreachable):
        r.setCondition(&kt, "Ready", metav1.ConditionFalse, "KafkaUnreachable", err.Error())
        _ = r.Status().Update(ctx, &kt)
        return ctrl.Result{RequeueAfter: 30 * time.Second}, nil

    case err != nil:
        return ctrl.Result{}, err

    default:
        // 토픽 존재 — drift 검사는 2주차
        return r.markReady(ctx, &kt, "TopicSynced")
    }
}
```

---

## 7. 동시성 / 안전성

- `KafkaClient` 구현체는 **goroutine-safe** 해야 한다. controller-runtime 은 동일 GVK에 대해 단일 worker가 기본이지만, MaxConcurrentReconciles 를 늘릴 가능성을 열어둔다.
- 내부적으로 admin client connection pool을 재사용한다 (매 호출마다 새 연결 X).

---

## 7-1. 배포 / 운영 컨텍스트

스터디 클러스터는 5개 팀이 공유한다 (`rule.md` 참조). 우리는 **2팀**으로, namespace `team-2`에서만 작업한다.

| 구성 요소 | 위치 / 범위 |
|---|---|
| Operator Deployment | `team-2` namespace |
| `KafkaTopic` CR | `team-2` namespace |
| `KafkaTopic` CRD | **클러스터 전역** (CRD는 본질적으로 cluster-scoped) — 임의 삭제 금지 |
| Kafka 접속 정보 (ConfigMap/Secret) | `team-2` namespace |
| API Group | `kafka.study.dev` (팀별 충돌 방지 규칙) |

**Controller scope 제한**: controller-runtime 의 `cache.Options{DefaultNamespaces: {"team-2": {}}}` 로 watch 범위를 `team-2`에 한정한다. 다른 팀이 실수로 같은 CRD를 사용해도 우리 operator는 반응하지 않는다.

**KafkaClient 자체는 namespace 개념을 모른다.** 외부 Kafka admin API와만 통신하며, namespace 분리는 controller 레이어의 책임이다. 따라서 이 인터페이스 자체는 공유 클러스터 규칙의 영향을 받지 않는다.

---

## 8. 테스트 전략

- 담당자 1: 실제 Kafka(docker-compose)로 통합 테스트 — 각 메서드의 happy path + 에러 케이스.
- 담당자 2: `internal/kafka/fake` 패키지에 in-memory `FakeClient` 구현 → controller 단위 테스트에서 사용.
- 통합 테스트는 1주차 후반에 mock → 실 client 교체로 검증.

```go
// 담당자 2가 사용할 fake 예시
type FakeClient struct {
    mu     sync.Mutex
    topics map[string]*TopicInfo
}
```

---

## 9. 오픈 이슈 / 합의 필요

- [ ] 브로커 접속 정보(`bootstrap.servers`, SASL 등)는 **어디서 주입**? — 후보:
  - (A) 환경변수 / flag (Operator Deployment에 주입) — 단일 Kafka 클러스터 가정
  - (B) `team-2` namespace의 ConfigMap/Secret 마운트 — credential 분리에 유리
  - (C) `KafkaTopic.Spec.BootstrapServers` — CR마다 다른 클러스터 가능
  - (D) `KafkaConnection` 같은 별도 CRD — 가장 유연하지만 범위 초과
  - **잠정 결정**: (A) flag/env — 1주차는 단순화. SASL 등 secret이 필요해지면 (B)로 확장.
  - **제약**: 어떤 방식이든 관련 리소스(ConfigMap/Secret)는 반드시 `team-2` namespace에 둔다. 공유 클러스터 규칙(`rule.md`).
- [ ] `UpdateConfig`에서 spec에 없는 key를 어떻게 처리? — Kafka 기본값으로 reset vs 무시. **잠정**: 무시 (spec에 명시된 key만 override).
- [ ] Replication factor 변경은 지원하지 않는다 (Kafka 자체가 `kafka-reassign-partitions` 별도 필요). 1차 범위 제외.

---

## 10. 변경 이력

- 2026-06-15: 초안 작성 (1주차 시작 전 합의용)
- 2026-06-15: 공유 클러스터 규칙(`rule.md`) 반영 — 배포/운영 컨텍스트 섹션 추가, 브로커 접속 정보 오픈 이슈에 `team-2` namespace 제약 명시
