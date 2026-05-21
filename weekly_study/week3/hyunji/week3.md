# Week 3 — Operator SDK & Kubebuilder

> **학습 목표**
> - SDK/Kubebuilder 사용 이유 분석
> - Scaffolding 프로젝트 구조 분석
> - Controller-runtime 라이브러리 이해


## Operator란?


### 1. 용어 정리: Controller vs Operator

**Controller** → Kubernetes가 이미 정의한 기본 리소스를 관리 (예: ReplicaSet Controller, Deployment Controller)

**Operator** → 사용자가 직접 정의한 커스텀 리소스를 관리 (예: MySQL Operator, Prometheus Operator)

> 실제로는 혼용해서 쓰는 경우가 많습니다.


### 2. Operator가 왜 필요한가? — Stateful Application 문제

지금까지 배운 Kubernetes의 기본 컨트롤러들(Deployment 등)은 **Stateless 앱**에 최적화되어 있습니다.

```
Stateless App (예: 웹 서버)
  → 그냥 Pod 복제하면 됨
  → Deployment Controller로 충분

Stateful App (예: MySQL, Kafka, Elasticsearch)
  → 단순 복제로 안 됨
  → 백업 스케줄링
  → 마스터 다운 시 새 마스터 선출 (Leader Election)
  → 데이터 볼륨 관리
  → 클러스터 멤버 등록/해제
  → 이 모든 "운영 지식"을 코드로 담아야 함  ← 이게 Operator
```

**Operator의 핵심 철학: 사람이 하던 운영 작업을 코드로 자동화**


### 3. 세 가지 핵심 개념: CRD / CR / Operator

**CRD (Custom Resource Definition)**
→ "이런 종류의 리소스가 존재한다"는 설계도/스펙 정의
→ Kind: CustomResourceDefinition

↓ CRD를 등록하면

**CR (Custom Resource)**
→ CRD 스펙에 맞게 실제로 생성한 리소스 인스턴스
→ 예: kind: HelloOperator, name: my-helloworld

↓ CR이 생성/변경/삭제되면

**Operator (비즈니스 로직)**
→ 이벤트를 감지하고 원하는 상태(Desired State)로 맞추는 실제 코드
→ Kubernetes API를 주기적으로 감시(Watch)




### 4. Operator의 동작 흐름 요약

```
1. CRD 등록       kubectl apply -f crd.yaml
        ↓
2. CR 생성         kubectl apply -f myresource.yaml
        ↓
3. Operator 감지   CR 생성/변경/삭제 이벤트를 Watch
        ↓
4. 로직 실행       원하는 상태(Desired State)로 맞추는 코드 실행
                  (Pod 생성, 백업 실행, 마스터 선출 등)
        ↓
5. 반복            Reconciliation Loop — 계속 상태를 감시하고 유지
```

이 **Reconciliation Loop**는 저번 주에 배운 개념과 완전히 동일합니다. Operator도 결국 같은 철학 위에 있습니다.


## Kubebuilder


### 1. Kubebuilder의 정의

```
Kubebuilder
= Kubernetes SIG(Special Interest Group)에서
  공식적으로 관리하는 Operator/Controller 개발 프레임워크

- 언어: Go
- 목적: CRD + Controller(Operator)를 빠르고 표준적으로 만들기 위한 도구
- 핵심: controller-runtime 라이브러리 위에서 동작
```

Kubebuilder는 controller-runtime 프로젝트의 도구들을 활용하여 Controller가 Kubernetes API와 상호작용하는 무거운 작업을 대신 처리해줍니다.


### 2. Raw 방식(client-go)의 한계 — 왜 Kubebuilder가 필요한가

**Raw client-go 방식으로 Operator를 만들 때 직접 해야 하는 것들:**

1. Informer / Watcher
2. Work Queue
3. Cache
4. CRD YAML 직접 작성
5. RBAC 권한 YAML 직접 작성
6. Leader Election 직접 구현
7. Metrics 엔드포인트 직접 구현


### 3. Kubebuilder가 해결해주는 것들

**자동으로 해주는 것 (Scaffolding)**

- 프로젝트 기본 구조 생성 (`kubebuilder init`)
- CRD YAML 자동 생성 (Go 코드 → YAML)
- RBAC 매니페스트 자동 생성
- Controller 기본 코드 생성
- Reconciliation Loop 틀 제공

**controller-runtime이 내부에서 처리해주는 것**

- Informer / Cache / Work Queue 관리
- Leader Election (HA 지원)
- Prometheus Metrics 엔드포인트
- Webhook 설정

Kubebuilder는 CRD 스펙을 자동으로 생성해줄 뿐만 아니라, RBAC과 샘플 리소스 매니페스트도 생성하고, CRD를 클러스터에 설치하며, Operator 이미지를 빌드/퍼블리시하고, Controller를 실행하는 것까지 지원합니다.


### 4. 전체 구조로 보는 위치

```
내가 짜는 코드 (비즈니스 로직 — Reconcile 함수)
        ↑
   Kubebuilder  (프로젝트 구조 + 코드 생성 도구)
        ↑
 controller-runtime  (Informer, Cache, Queue 등 처리)
        ↑
   client-go  (Kubernetes API 통신 최하층)
        ↑
 Kubernetes API Server
```

| | 하는 일 |
|--|---------|
| **client-go** | API 서버와 실제 통신 |
| **controller-runtime** | client-go 컴포넌트들을 추상화 + Controller 실행 환경 제공 |
| **Kubebuilder** | controller-runtime 기반 프로젝트 뼈대 자동 생성 |


## Scaffolding 프로젝트 구조 분석


### 1. Scaffolding이란?

```bash
kubebuilder init --domain example.com --repo example.com/myoperator
```

이 명령 하나를 실행하면 Kubebuilder가 **프로젝트 전체 뼈대를 자동 생성**해줍니다. 이걸 **Scaffolding**이라고 합니다.


### 2. 생성되는 전체 디렉토리 구조

```
myoperator/
│
├── cmd/
│   └── main.go              ← 프로그램 진입점 (Manager 실행)
│
├── internal/
│   └── controller/
│       └── myresource_controller.go  ← Reconcile 로직 작성 위치
│
├── api/
│   └── v1/
│       └── myresource_types.go  ← CRD 스펙 정의 (Go struct)
│
├── config/
│   ├── default/             ← 기본 Kustomize 설정
│   ├── manager/             ← Controller를 Pod로 실행하는 설정
│   ├── rbac/                ← RBAC 권한 설정
│   └── crd/                 ← 자동 생성된 CRD YAML
│
├── PROJECT                  ← Kubebuilder 메타데이터 파일
├── Makefile                 ← 빌드/배포 자동화 명령어 모음
├── Dockerfile               ← Controller 컨테이너 이미지 빌드용
└── go.mod                   ← Go 모듈 의존성 정의
```


### 3. 각 파일/디렉토리 상세 설명

#### `go.mod` — Go 모듈 의존성

새 Go 모듈로 프로젝트를 초기화하며, 기본 의존성이 포함됩니다. 핵심 의존성은 `k8s.io/api`, `k8s.io/apimachinery`, `k8s.io/client-go`, `sigs.k8s.io/controller-runtime`입니다.

```go
require (
    k8s.io/api v0.35.0
    k8s.io/apimachinery v0.35.0
    k8s.io/client-go v0.35.0
    sigs.k8s.io/controller-runtime v0.23.3
)
```

#### `PROJECT` — Kubebuilder 메타데이터

이 파일은 프로젝트를 scaffold하는 데 사용된 정보를 추적하고 플러그인이 올바르게 작동할 수 있도록 합니다.

```yaml
domain: tutorial.kubebuilder.io
layout:
- go.kubebuilder.io/v4        # 사용한 플러그인 버전
projectName: project
resources:
- group: batch
  kind: CronJob
  version: v1
```

→ `kubebuilder create api` 명령을 실행할 때마다 이 파일에 리소스 정보가 추가됩니다.

#### `Makefile` — 자동화 명령어 모음

Controller 아티팩트를 빌드, 테스트, 실행, 배포하기 위한 Make 타겟이 포함되어 있습니다.

```bash
make manifests   # Go 코드 → CRD/RBAC YAML 자동 생성
make generate    # DeepCopy 함수 자동 생성
make build       # 바이너리 빌드
make run         # 로컬에서 Controller 실행 (개발용)
make docker-build # 컨테이너 이미지 빌드
make deploy      # 클러스터에 배포
make install     # CRD를 클러스터에 설치
```

#### `config/` — 클러스터 배포 설정

현재는 Controller를 클러스터에서 실행하는 데 필요한 Kustomize YAML 정의가 포함되어 있으며, Controller 작성을 시작하면 CRD, RBAC 설정, WebhookConfiguration도 이 디렉토리에 추가됩니다.

```
config/
├── default/   ← 표준 설정으로 Controller를 실행하는 Kustomize base
├── manager/   ← Controller를 클러스터의 Pod로 실행
└── rbac/      ← 자체 서비스 계정으로 Controller 실행에 필요한 권한
```

#### `cmd/main.go` — 프로그램 진입점

main.go에서는 Manager를 초기화합니다. Manager는 모든 Controller의 실행을 추적하고, API 서버에 대한 공유 캐시와 클라이언트를 설정합니다. Manager는 graceful shutdown 신호를 받을 때까지 실행됩니다.

```go
func main() {
    // 1. Manager 생성 (Controller들의 관리자)
    mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
        Scheme:           scheme,
        LeaderElection:   enableLeaderElection,  // HA 지원
        // ...
    })

    // 2. Controller 등록
    // +kubebuilder:scaffold:builder  ← 새 API 추가 시 자동으로 코드 삽입되는 위치

    // 3. Manager 실행 (모든 Controller + Webhook 실행)
    mgr.Start(ctrl.SetupSignalHandler())
}
```

main.go는 controller-runtime 핵심 라이브러리와 기본 로깅 라이브러리인 Zap을 import합니다.


### 4. `+kubebuilder:scaffold` 마커란?

`+kubebuilder:scaffold` 마커는 Kubebuilder scaffolding 시스템의 핵심 부분으로, 새로운 리소스(Controller, Webhook, API 등)를 scaffold할 때 Kubebuilder가 추가 코드를 삽입할 위치를 표시합니다. 이를 통해 사용자가 정의한 코드에 영향을 주지 않으면서 새로 생성된 컴포넌트를 프로젝트에 자연스럽게 통합할 수 있습니다.

```go
// main.go 안에 있는 마커들 예시

import (
    crewv1 "..."
    // +kubebuilder:scaffold:imports  ← 새 API import가 여기 자동 삽입
)

func init() {
    utilruntime.Must(crewv1.AddToScheme(scheme))
    // +kubebuilder:scaffold:scheme   ← 새 API 스킴 등록 코드 자동 삽입
}

func main() {
    // +kubebuilder:scaffold:builder  ← 새 Controller 등록 코드 자동 삽입
}
```

> 마커를 삭제하거나 이동시키면 CLI가 필요한 코드를 삽입할 수 없게 되어 scaffolding 과정이 실패하거나 예상치 못한 동작이 발생할 수 있습니다.


### 5. `kubebuilder create api` 실행 후 추가되는 구조

`init` 이후 `kubebuilder create api --group batch --version v1 --kind CronJob`을 실행하면:

```
추가/변경되는 것들:

api/v1/
└── cronjob_types.go        ← 내가 직접 CRD 필드를 정의하는 파일

internal/controller/
└── cronjob_controller.go   ← 내가 Reconcile 로직을 작성하는 파일

config/crd/bases/
└── batch.tutorial.kubebuilder.io_cronjobs.yaml  ← make manifests 후 자동 생성

main.go
└── (마커 위치에 Controller 등록 코드 자동 삽입)
```


### 6. 전체 흐름 한눈에 보기

```
kubebuilder init
    → 프로젝트 뼈대 생성 (go.mod, Makefile, config/, cmd/main.go)

kubebuilder create api
    → api/v1/*_types.go      (CRD 정의 파일)
    → internal/controller/*  (Reconcile 로직 파일)
    → main.go에 scaffold 마커로 코드 자동 삽입

개발자가 할 일
    → *_types.go에 원하는 필드 추가
    → *_controller.go의 Reconcile 함수에 비즈니스 로직 작성

make manifests
    → Go 코드 → CRD/RBAC YAML 자동 생성

make deploy
    → 클러스터에 배포
```


## Controller-runtime 라이브러리


### 1. controller-runtime이란?

> controller-runtime은 Kubernetes 스타일의 Controller를 구축하기 위한 Go 라이브러리 모음으로, Kubernetes CRD와 내장 Kubernetes API를 모두 조작할 수 있습니다. Kubebuilder와 Operator SDK 모두 이 라이브러리를 기반으로 만들어졌습니다.


### 2. 핵심 구성요소 전체 지도

controller-runtime의 주요 구성 요소는 Manager, Controller, Reconciler, Client/Cache, Scheme, Webhook, Logging/Metrics입니다.

```
┌───────────────────────────────────────────────────────────────┐
│                        Manager                                │
│  (모든 Controller와 Webhook의 실행을 총괄하는 최상위 관리자)    │
│                                                               │
│  ┌─────────────────┐   ┌─────────────────┐                   │
│  │   Controller    │   │   Controller    │  ...              │
│  │  ┌───────────┐  │   │  ┌───────────┐  │                   │
│  │  │ Reconciler│  │   │  │ Reconciler│  │                   │
│  │  └───────────┘  │   │  └───────────┘  │                   │
│  └─────────────────┘   └─────────────────┘                   │
│                                                               │
│  공유 자원: Cache │ Client │ Scheme │ LeaderElection          │
└───────────────────────────────────────────────────────────────┘
```


### 3. 핵심 구성요소 상세

#### ① Manager — 총괄 관리자

모든 Controller와 Webhook은 궁극적으로 Manager에 의해 실행됩니다. Manager는 Controller와 Webhook을 실행하고, 공유 캐시와 클라이언트 같은 공통 의존성을 설정하며, Leader Election을 관리합니다. Manager는 일반적으로 Pod 종료 시 Controller를 graceful shutdown하도록 시그널 핸들러와 연결됩니다.

```go
// main.go에서 Manager를 이렇게 생성
mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
    Scheme:           scheme,
    LeaderElection:   true,   // HA 환경에서 중복 실행 방지
})

// Manager가 시작되면 등록된 모든 Controller가 함께 시작됨
mgr.Start(ctrl.SetupSignalHandler())
```

#### ② Controller — 이벤트 감지 → Reconcile 요청 전달

Controller는 이벤트를 사용하여 최종적으로 Reconcile 요청을 트리거합니다. Builder를 통해 이벤트 소스(Kubernetes API 오브젝트 변경 등)를 이벤트 핸들러에 연결하는 작업을 쉽게 할 수 있으며, Predicate를 사용하여 실제로 Reconcile을 트리거하는 이벤트를 필터링할 수 있습니다.

Controller는 `source.Sources`에서 전달받은 `reconcile.Request`를 처리하는 Work Queue를 관리합니다. 작업은 각 큐 항목에 대해 `reconcile.Reconciler`를 통해 수행되며, 일반적으로 Kubernetes 오브젝트를 읽고 써서 오브젝트 Spec에 지정된 상태와 시스템 상태를 일치시킵니다.

```
이벤트 흐름:

Kubernetes API 변경 (Create/Update/Delete)
        ↓
   Source (이벤트 감지)
        ↓
   Handler (이벤트 → Request로 변환)
        ↓
   Predicate (필터링: 이 이벤트가 Reconcile 필요한가?)
        ↓
   Work Queue (Request 적재)
        ↓
   Reconciler (비즈니스 로직 실행)
```

```go
// Controller 등록 예시 (Builder 패턴)
ctrl.NewControllerManagedBy(mgr).
    For(&batchv1.CronJob{}).      // 주 감시 대상
    Owns(&batchv1.Job{}).         // 소유하는 하위 리소스도 감시
    Complete(r)                   // Reconciler 연결
```

#### ③ Reconciler — 개발자가 직접 구현하는 핵심

Controller 로직은 Reconciler로 구현됩니다. Reconciler는 Reconcile할 오브젝트의 name과 namespace를 담은 Request를 받아 오브젝트를 Reconcile하고, 두 번째 처리를 위해 재큐잉할지 여부를 나타내는 Response 또는 error를 반환하는 함수를 구현합니다.

Reconciliation은 레벨 기반(level-based)으로 동작합니다. 즉, 개별 이벤트의 변경 사항이 아니라 API 서버 또는 로컬 캐시에서 읽은 실제 클러스터 상태에 의해 동작이 결정됩니다.

```go
// 개발자가 구현하는 Reconcile 함수
func (r *CronJobReconciler) Reconcile(
    ctx context.Context,
    req ctrl.Request,        // name + namespace만 포함 (이벤트 내용 없음)
) (ctrl.Result, error) {

    // 1. 현재 상태 읽기
    cronJob := &batchv1.CronJob{}
    r.Get(ctx, req.NamespacedName, cronJob)

    // 2. 비즈니스 로직 (Desired State로 맞추는 작업)
    // ...

    // 3. 결과 반환
    return ctrl.Result{}, nil                              // 정상 완료
    return ctrl.Result{Requeue: true}, nil                 // 다시 실행 요청
    return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil // 5분 후 재실행
}
```

**중요한 설계 원칙:**

Reconciler에는 name/namespace만 제공되며, 이벤트 내용이나 이벤트 타입은 포함되지 않습니다. 동일한 name/namespace에 대한 Reconcile Request는 큐에 쌓일 때 배치 처리되고 중복 제거됩니다. 이를 통해 Controller가 단일 오브젝트에 대한 대용량 이벤트를 우아하게 처리할 수 있습니다.

> **왜 이벤트 내용을 안 줄까?**
>
> "어떤 필드가 바뀌었는가" 보다 "지금 실제 상태가 어떤가"를 보고 판단하기 위해서
>
> → 항상 API 서버에서 최신 상태를 직접 조회
> → 이벤트가 100번 발생해도 Reconcile은 1번만 실행될 수 있음 (중복 제거 덕분에)

#### ④ Client & Cache — API 접근 방식

Reconciler는 Client를 사용하여 API 오브젝트에 접근합니다. Manager가 제공하는 기본 Client는 로컬 공유 캐시에서 읽고 API 서버에 직접 씁니다. Cache는 감시 중인 오브젝트로 자동 채워집니다.

```
읽기 (Get/List) → 로컬 Cache 사용  ← API 서버 부하 감소
쓰기 (Create/Update/Delete) → API 서버 직접

                  Cache (로컬)
                 ↗  (읽기)
Reconciler — Client
                 ↘  (쓰기)
                  API Server
```

#### ⑤ Scheme — Go 타입 ↔ Kubernetes Kind 매핑

Scheme은 Go 타입을 Kubernetes API Kind(Group-Version-Kind)에 연결합니다. Client, Cache 등 많은 곳에서 Scheme을 사용합니다.

> **Go 구조체가 API 서버에서 어떤 URL로 요청해야 하는지** 알려주는 매핑 테이블

```go
// init()에서 Scheme에 타입 등록
utilruntime.Must(clientgoscheme.AddToScheme(scheme))
utilruntime.Must(batchv1.AddToScheme(scheme))
// +kubebuilder:scaffold:scheme
```

> **왜 필요한가?**
> "이 Go struct가 Kubernetes에서 어떤 Kind인지"를 Client와 Cache가 알아야 API 통신을 할 수 있기 때문

#### ⑥ 그 외 구성요소 요약

| 구성요소 | 역할 |
|----------|------|
| **Predicate** | 이벤트 필터 — 특정 조건의 이벤트만 Reconcile 트리거 |
| **Leader Election** | HA 환경에서 Controller Pod가 여러 개여도 1개만 실행 |
| **Webhook** | CR 생성/수정 전 검증(Validation) 또는 기본값 설정(Defaulting) |
| **Metrics** | Prometheus 메트릭 자동 노출 |
| **envtest** | 실제 API 서버 + etcd를 로컬에 띄워 통합 테스트 지원 |


### 4. 전체 동작 흐름 한눈에 보기

```
┌─────────────────────────────────────────────────────────┐
│  kubectl apply -f myresource.yaml                       │
│         ↓                                               │
│  Kubernetes API Server (CR 저장)                        │
│         ↓                                               │
│  Source (변경 이벤트 감지)                               │
│         ↓                                               │
│  Predicate (이 이벤트가 처리 대상인가? 필터링)           │
│         ↓                                               │
│  Work Queue (Request 적재 + 중복 제거)                   │
│         ↓                                               │
│  Reconciler.Reconcile(ctx, Request)                     │
│    └─ Client.Get() → Cache에서 현재 상태 조회            │
│    └─ 비즈니스 로직 실행                                 │
│    └─ Client.Update() → API 서버에 새 상태 반영          │
│         ↓                                               │
│  Result{} 반환 → 완료 or Requeue                        │
└─────────────────────────────────────────────────────────┘
```


## Week 3 전체 내용 연결 정리

```
Operator 개념
    ↓  (CRD + CR + 비즈니스 로직을 직접 만들어야 함)
왜 Kubebuilder?
    ↓  (Raw client-go는 인프라 코드가 너무 많음)
Kubebuilder Scaffolding
    ↓  (프로젝트 뼈대 + 코드 자동 생성)
controller-runtime
    ↓  (Manager / Controller / Reconciler / Client / Cache)
개발자가 할 일
    → Reconcile 함수 안에 비즈니스 로직만 작성
```


## References

- https://bcho.tistory.com/1391
- https://sdk.operatorframework.io/docs/faqs/
- https://dev.to/thenjdevopsguy/what-is-a-kubernetes-operator-53kb
- https://enix.io/en/blog/kubebuilder/
- https://book.kubebuilder.io/cronjob-tutorial/basic-project.html
- https://book.kubebuilder.io/cronjob-tutorial/empty-main.html
- https://pkg.go.dev/sigs.k8s.io/controller-runtime
- https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg
- https://book.kubebuilder.io/cronjob-tutorial/controller-overview.html
