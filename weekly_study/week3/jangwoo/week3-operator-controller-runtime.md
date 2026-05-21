# Week 3 - Operator 패턴과 controller-runtime

---

## 이 문서의 흐름

Week2에서는 Kubernetes Controller가 **원하는 상태(desired state)** 와 **현재 상태(current state)** 를 계속 비교하며 클러스터를 조정한다는 Reconciliation 패턴을 봤다.

Week3에서는 이 패턴을 한 단계 확장한다.

```
Controller 패턴
  → CRD로 Kubernetes API를 확장
  → Custom Controller가 CR을 감시
  → Operator가 애플리케이션 운영을 자동화
  → controller-runtime으로 구현을 단순화
```

따라서 이 문서는 다음 순서로 읽으면 된다.

1. Operator가 무엇을 자동화하는지 이해한다.
2. Operator를 만들 때 사용하는 도구와 프로젝트 구조를 본다.
3. controller-runtime의 핵심 컴포넌트를 이해한다.
4. `main.go`에서 `Reconcile()`까지 실제 실행 흐름을 연결한다.

---

## 배경: 왜 Operator 개발 도구가 필요한가

Operator는 결국 **반복되는 운영 판단을 Kubernetes 안에서 자동으로 실행하는 프로그램**이다.

예를 들어 이미지 업로드를 처리하는 워커 서비스를 운영한다고 해보자. 트래픽이 적을 때는 워커를 적게 띄우고, 처리량이 밀리면 워커를 늘리고, 새 이미지 버전이 나오면 순서대로 교체해야 한다.

```text
운영자가 계속 확인하던 것
  - 워커 Pod가 원하는 개수만큼 떠 있는가
  - 이미지 버전이 선언한 값과 같은가
  - 처리 중인 작업이 있는데 바로 줄여도 되는가
  - 설정이 바뀌었을 때 안전하게 다시 띄웠는가
  - 현재 상태를 어디에 기록하고 확인할 것인가
```

Operator는 이런 판단을 Custom Resource와 Reconcile 로직으로 옮긴다.

```text
사용자
  → ImageWorker CR에 원하는 상태 선언
    replicas: 3
    image: worker:v2

Operator
  → Deployment가 없으면 생성
  → replicas나 image가 다르면 수정
  → Ready 개수를 status에 기록
```

이론적으로는 Week2에서 본 `client-go`만으로도 Operator를 만들 수 있다. 하지만 실제 Operator를 만들려면 비즈니스 로직 외에도 반복적인 기반 코드가 많이 필요하다.

```text
Operator마다 반복되는 기반 작업
├── 새 API 타입과 CRD 스키마 만들기
├── Watch 대상 등록하기
├── 이벤트를 Queue에 넣고 중복 처리하기
├── Reconcile 실행과 재시도 처리하기
├── status 서브리소스와 RBAC 맞추기
├── 여러 복제본 실행 시 leader만 동작하게 하기
├── metrics와 health endpoint 열기
└── 배포 매니페스트 구성하기
```

이 부분을 매번 직접 만들면 정작 중요한 **도메인 운영 로직**보다 주변 코드가 더 커지기 쉽다. 그래서 Operator 개발에서는 반복되는 구조를 도구와 라이브러리에 맡기고, 개발자는 `spec/status` 설계와 `Reconcile()` 구현에 집중한다.

그래서 `Kubebuilder`, `Operator SDK`, `controller-runtime`을 사용한다.

```text
controller-runtime
  → Informer, Workqueue, Client, Manager 같은 공통 구조 제공

Kubebuilder / Operator SDK
  → 프로젝트 구조, CRD 타입, RBAC, Controller 템플릿 생성

개발자
  → spec/status 설계와 Reconcile 로직에 집중
```

이 문서의 핵심은 “Operator가 무엇인가”뿐 아니라, **왜 controller-runtime이 Operator 개발의 표준 기반이 되었는가**를 이해하는 것이다.

---

## 1. Operator Pattern

### 1-1. Operator란 무엇인가

`Operator`는 **애플리케이션 운영 지식을 코드로 만든 Kubernetes Controller**다.

일반 Controller가 Deployment, Pod 같은 기본 리소스를 조정한다면, Operator는 DB, Kafka, Prometheus처럼 운영 절차가 복잡한 애플리케이션을 CRD와 Controller로 관리한다.

```
사용자
  → Custom Resource에 원하는 상태 선언

Operator
  → CR을 Watch
  → 현재 상태 조회
  → 필요한 하위 리소스 생성/수정
  → status에 관찰 결과 기록
```

예를 들어 사용자는 Postgres 클러스터를 직접 Pod, PVC, Service로 나누어 만들지 않고 다음처럼 선언할 수 있다.

```yaml
apiVersion: acid.zalan.do/v1
kind: postgresql
metadata:
  name: my-postgres
spec:
  numberOfInstances: 3
  postgresql:
    version: "15"
```

Operator는 이 CR을 보고 필요한 StatefulSet, Service, Secret, 백업 설정 등을 만들고 유지한다.

---

### 1-2. Operator가 해결하는 문제

Kubernetes의 기본 리소스만으로도 단순한 배포는 가능하다. 하지만 운영이 길어질수록 사람의 판단과 반복 작업이 늘어난다.

처음에는 `Deployment`와 `Service`만으로 충분해 보일 수 있다. 하지만 실제 운영에서는 “버전을 언제 올릴지”, “장애가 났을 때 어떤 순서로 복구할지”, “현재 상태를 어디에 기록할지” 같은 판단이 계속 생긴다. Operator는 이런 판단을 사람이 매번 실행하는 대신, Controller의 Reconcile 로직 안에 넣어 자동화한다.

**부록: Day-N이란?** `Day-0`, `Day-1`, `Day-2`처럼 시스템의 설계부터 운영까지의 단계를 시간 순서로 나누어 부르는 표현이다.


| 단계    | 의미    | 예시                    | Operator의 역할                |
| ----- | ----- | --------------------- | --------------------------- |
| Day-0 | 설계/개발 | CRD 설계, Controller 구현 | Operator 자체를 만든다            |
| Day-1 | 최초 설치 | 앱 배포, 초기 리소스 생성       | CR 선언만으로 설치를 자동화한다          |
| Day-2 | 지속 운영 | 업그레이드, 백업, 장애 복구      | 운영 절차를 Reconcile 로직으로 자동화한다 |


Operator의 핵심 가치는 Day-2 운영에 있다.

```
단순 설치 자동화      → Helm, Kustomize로도 충분한 경우가 많음
지속 운영 자동화      → Operator가 필요한 지점
```

---

### 1-3. CRD 짧은 복습

`CRD(Custom Resource Definition)`는 Kubernetes API에 **새로운 리소스 타입을 추가하는 방법**이다.

Kubernetes에는 기본적으로 `Pod`, `Deployment`, `Service` 같은 Built-in Resource가 있다. 여기에 `MyApp`, `RedisCluster`, `ImageWorker`처럼 우리 서비스에 맞는 리소스 타입을 추가하고 싶을 때 CRD를 사용한다.

```text
Built-in Resource
  - Pod
  - Deployment
  - Service

Custom Resource
  - MyApp
  - RedisCluster
  - ImageWorker
```

CRD와 Custom Resource는 구분해서 이해해야 한다.

| 개념 | 의미 | 예시 |
| --- | --- | --- |
| CRD | 새로운 리소스 타입의 정의 | `kind: CustomResourceDefinition` |
| Custom Resource(CR) | CRD를 바탕으로 사용자가 실제로 만든 리소스 | `kind: MyApp` |

비유하면 CRD는 “설문지 양식”이고, Custom Resource는 “사용자가 작성한 설문지”에 가깝다. CRD가 먼저 등록되어 있어야 사용자는 그 타입의 CR을 생성할 수 있다.

```text
CRD 등록
  -> Kubernetes API에 MyApp이라는 타입 추가
  -> kubectl get myapps 가능
  -> 사용자가 MyApp CR 생성 가능
```

Custom Resource도 Kubernetes 리소스이므로 `spec`과 `status` 구조를 가진다.

```yaml
apiVersion: apps.example.com/v1alpha1
kind: MyApp
metadata:
  name: sample
spec:
  replicas: 2      # 사용자가 원하는 상태
  image: nginx
status:
  readyReplicas: 1 # Controller가 관찰한 현재 상태
```

여기서 중요한 규칙은 단순하다.

| 필드 | 쓰는 주체 | 의미 |
| --- | --- | --- |
| `spec` | 사용자, GitOps 도구 | 원하는 상태 |
| `status` | Controller, Operator | 현재 관찰된 상태 |

즉, CRD는 “무엇을 선언할 수 있는가”를 정하고, Controller는 그 선언을 실제 클러스터 상태로 맞춘다. 이 둘을 함께 묶으면 Operator가 된다.

---

### 1-4. Operator의 구성요소

Operator는 크게 두 가지로 구성된다.

```
Operator = CRD + Custom Controller
```

CRD는 사용자가 Kubernetes에 새로 추가하는 API의 모양을 정의한다. 예를 들어 `MyApp`이라는 CRD를 만들면, 사용자는 `kubectl apply -f myapp.yaml`처럼 Kubernetes 기본 리소스와 같은 방식으로 `MyApp` 리소스를 생성할 수 있다.

Custom Controller는 이 `MyApp` 리소스를 계속 지켜보다가, 사용자가 선언한 상태와 실제 클러스터 상태가 다르면 필요한 작업을 수행한다. 즉, CRD가 “무엇을 선언할 수 있는가”를 정한다면, Controller는 “그 선언을 실제로 어떻게 맞출 것인가”를 담당한다.


| 구성요소              | 역할                                   |
| ----------------- | ------------------------------------ |
| CRD               | 사용자가 선언할 새로운 Kubernetes API 타입을 정의한다 |
| Custom Resource   | 사용자가 실제로 생성하는 원하는 상태 문서다             |
| Custom Controller | CR을 Watch하고 Reconcile을 실행한다          |


동작 흐름은 Week2의 Controller와 같다.

```
1. 사용자가 CR apply
2. Controller가 CR 변경 감지
3. Reconcile에서 desired/current 비교
4. Deployment, Service 같은 하위 리소스 생성/수정
5. CR status 업데이트
```

중요한 점은 Operator가 이벤트 자체에 반응하는 것이 아니라, 이벤트를 계기로 **현재 상태를 다시 조회하고 원하는 상태와 맞춘다**는 것이다.

---

### 1-5. Operator 성숙도 모델

모든 Operator가 같은 수준의 자동화를 제공하지는 않는다. 어떤 Operator는 설치만 자동화하고, 어떤 Operator는 백업/복원/장애 복구까지 처리한다.

Operator 성숙도 모델은 “이 Operator가 운영을 어디까지 대신하는가”를 나누는 기준이다.


| Level | 이름               | 핵심 의미                     |
| ----- | ---------------- | ------------------------- |
| 1     | Basic Install    | CR을 보고 하위 리소스를 생성한다       |
| 2     | Seamless Upgrade | 버전 업그레이드와 롤백을 처리한다        |
| 3     | Full Lifecycle   | 백업, 복원, 장애 복구까지 관리한다      |
| 4     | Deep Insights    | 메트릭, 이벤트, 상태 정보를 운영에 활용한다 |
| 5     | Auto Pilot       | 부하와 정책을 보고 자동 튜닝한다        |


학습 단계에서는 Level 1을 정확히 이해하는 것이 중요하다.

```
CR 생성
  → Deployment 생성
  → spec 변경 시 Deployment 수정
  → status에 현재 상태 기록
```

이 기본 흐름이 잡히면 Level 2 이상의 기능은 Reconcile 로직을 확장하는 문제로 볼 수 있다. -> Week 4 주제

---

## 2. Operator 개발 도구

Operator를 직접 만들 때는 보통 `Kubebuilder` 또는 `Operator SDK`를 사용한다. 둘 다 내부적으로 `controller-runtime`을 기반으로 한다.

여기서 중요한 점은 Kubebuilder와 Operator SDK가 Operator의 동작 원리를 바꾸는 도구는 아니라는 것이다. Operator는 여전히 CRD와 Controller로 동작한다. 다만 프로젝트 구조, CRD 생성, RBAC 생성, Controller 등록 같은 반복 작업을 미리 만들어 주기 때문에 개발자는 핵심 로직에 더 빨리 들어갈 수 있다.

```
Kubebuilder / Operator SDK
  → 프로젝트 구조 생성
  → CRD 타입 코드 생성
  → Controller 템플릿 생성
  → controller-runtime 기반으로 실행
```

---

### 2-1. controller-runtime이란

`controller-runtime`은 Kubernetes Controller/Operator 개발을 쉽게 해주는 Go 라이브러리다.

Week2에서 직접 다뤘던 Informer, Workqueue, EventHandler, Client 호출을 더 높은 수준의 API로 묶어준다.


| Week2 client-go  | controller-runtime   |
| ---------------- | -------------------- |
| Informer / Store | Cache                |
| EventHandler     | Controller의 Watch 설정 |
| Workqueue        | Controller 내부 Queue  |
| reconcile 함수     | Reconciler 인터페이스     |
| clientset        | Client               |


즉, 개발자는 저수준 컴포넌트를 직접 조립하기보다 `Reconcile()`에 도메인 로직을 집중한다.

---

### 2-2. Kubebuilder와 Operator SDK


| 기준      | Kubebuilder           | Operator SDK                      |
| ------- | --------------------- | --------------------------------- |
| 주 사용 언어 | Go                    | Go, Ansible, Helm                 |
| 기반      | controller-runtime    | controller-runtime                |
| 특징      | Kubernetes 표준에 가까운 구조 | OLM, OperatorHub 통합이 강함           |
| 적합한 경우  | Go로 새 Operator 개발     | Helm/Ansible 자산 활용, OpenShift 생태계 |


학습과 Go 기반 Operator 개발에는 Kubebuilder 흐름을 먼저 이해하는 것이 좋다.

```bash
kubebuilder init --domain example.com --repo github.com/myorg/my-operator
kubebuilder create api --group apps --version v1alpha1 --kind MyApp
```

이 명령을 실행하면 CRD 타입, Controller, RBAC, 배포 매니페스트의 기본 뼈대(Scaffolding)가 생성된다.

---

## 3. Kubebuilder 프로젝트 구조

Kubebuilder가 생성하는 프로젝트는 Operator 개발에 필요한 파일을 미리 나누어 둔다.

처음 보면 파일이 많아 보여도 전부 같은 비중으로 이해할 필요는 없다. 학습 단계에서는 “내가 직접 작성해야 하는 파일”과 “도구가 생성해 주는 파일”을 구분하는 것이 먼저다. 대부분의 실습은 `api/` 아래의 타입 정의와 `internal/controller/` 아래의 Reconcile 구현에서 이루어진다.

```text
my-operator/
├── api/v1alpha1/
│   ├── myapp_types.go
│   ├── groupversion_info.go
│   └── zz_generated.deepcopy.go
├── cmd/
│   └── main.go
├── internal/controller/
│   └── myapp_controller.go
├── config/
│   ├── crd/
│   ├── default/
│   ├── manager/
│   ├── rbac/
│   └── samples/
├── Dockerfile
├── Makefile
├── PROJECT
└── go.mod
```

모든 폴더와 파일이 들어가 있지 않지만, 기본 구조는 들어가 있다. Kubebuilder 버전이나 옵션에 따라 `config/prometheus/`, `config/webhook/`, `test/` 같은 디렉토리가 더 생길 수 있지만, 처음 학습할 때는 위 구조를 먼저 잡으면 충분하다.

위 구조에서 모든 파일을 같은 깊이로 볼 필요는 없다. 처음에는 아래 흐름만 잡으면 된다.

```text
api/.../types.go
  -> CRD API 모양 정의
  -> make manifests
  -> config/crd/ 아래 CRD YAML 생성

internal/controller/..._controller.go
  -> Reconcile 로직 작성
  -> CR을 보고 Deployment 같은 하위 리소스 조정

cmd/main.go
  -> Manager 생성
  -> Controller 등록
  -> Operator 실행
```

---

### 3-1. 개발자가 직접 작성하는 파일


| 파일/디렉토리                               | 역할                             | 개발자가 하는 일            |
| ------------------------------------- | ------------------------------ | -------------------- |
| `api/v1alpha1/*_types.go`             | CRD의 `spec`, `status` Go 타입 정의 | API 모양을 직접 작성        |
| `internal/controller/*_controller.go` | Reconcile 로직 구현                | 운영 자동화 로직을 직접 작성     |
| `cmd/main.go`                         | Manager 생성, Controller 등록      | 필요한 설정만 조정           |
| `config/crd/`                         | CRD YAML 생성물                   | `make manifests`로 생성 |
| `config/rbac/`                        | RBAC 생성물                       | marker로 관리           |


핵심은 두 파일이다.

```
api/.../types.go
  → 사용자가 어떤 spec을 선언할 수 있는지 정의

internal/controller/..._controller.go
  → 그 spec을 실제 리소스로 어떻게 맞출지 구현
```

즉, `types.go`는 사용자에게 보여 줄 API를 설계하는 곳이고, `controller.go`는 그 API를 실제 Kubernetes 리소스로 바꾸는 곳이다. Operator를 처음 공부할 때는 이 두 파일의 역할만 명확히 잡아도 전체 구조가 훨씬 쉽게 보인다.

직접 작성하지 않더라도 알아두면 좋은 파일도 있다.


| 파일/디렉토리                    | 알아야 하는 이유                     |
| -------------------------- | ----------------------------- |
| `groupversion_info.go`     | 커스텀 타입을 Scheme에 등록하는 연결 지점    |
| `zz_generated.deepcopy.go` | Kubernetes 객체 복사용 자동 생성 코드    |
| `config/default/`          | 배포 시 사용할 Kustomize 기본 묶음      |
| `config/manager/`          | Operator Deployment 매니페스트     |
| `config/samples/`          | 사용자가 apply할 CR 예시             |
| `PROJECT`                  | Kubebuilder가 프로젝트 정보를 기록하는 파일 |
| `go.mod`                   | Go module과 의존성 정보             |
| `Dockerfile`               | Operator 이미지를 만들 때 사용         |




Kubebuilder 프로젝트에는 자주 쓰는 작업을 `Makefile` 명령으로 묶어 둔다. 처음에는 아래 명령 정도만 알아도 실습을 따라갈 수 있다.


| 명령어                         | 언제 쓰나                   | 결과                                        |
| --------------------------- | ----------------------- | ----------------------------------------- |
| `make generate`             | Go 타입을 바꾼 뒤             | `zz_generated.deepcopy.go` 생성/갱신          |
| `make manifests`            | marker나 RBAC 주석을 바꾼 뒤   | CRD/RBAC YAML 생성/갱신                       |
| `make install`              | CRD를 클러스터에 등록할 때        | `config/crd/`의 CRD가 클러스터에 설치됨             |
| `make uninstall`            | 실습 후 CRD를 제거할 때         | 클러스터에서 CRD 제거                             |
| `make run`                  | Operator를 로컬에서 실행할 때    | 현재 kubeconfig 대상 클러스터를 바라보며 Controller 실행 |
| `make docker-build IMG=...` | Operator 이미지를 만들 때      | 컨테이너 이미지 빌드                               |
| `make deploy IMG=...`       | Operator를 클러스터 안에 배포할 때 | `config/manager`, `config/rbac` 기반으로 배포   |
| `make undeploy`             | 배포한 Operator를 제거할 때     | 클러스터 안의 Operator Deployment/RBAC 제거       |


흐름으로 보면 보통 이렇게 사용한다.

```text
types.go 수정
  -> make generate
  -> make manifests
  -> make install
  -> make run
```

---

### 3-2. CRD 타입 파일의 핵심

`*_types.go`는 Custom Resource의 형태를 Go 타입으로 정의한다.

Kubernetes에서 모든 리소스는 `spec`과 `status`를 중심으로 이해할 수 있다. `spec`은 사용자가 “이렇게 되어야 한다”고 선언하는 값이고, `status`는 Controller가 “현재 실제로는 이렇게 되어 있다”고 기록하는 값이다. Operator 개발에서도 이 구분이 가장 중요하다.

```go
// MyAppSpec은 사용자가 선언하는 원하는 상태다.
type MyAppSpec struct {
    // 만들고 싶은 Pod 개수
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=5
    // +kubebuilder:default=1
    Replicas int32 `json:"replicas,omitempty"`

    // 배포할 컨테이너 이미지
    // +kubebuilder:validation:Required
    Image string `json:"image"`
}

// MyAppStatus는 Controller가 기록하는 현재 상태다.
type MyAppStatus struct {
    // 실제 Ready 상태가 된 Pod 개수
    ReadyReplicas int32 `json:"readyReplicas,omitempty"`
}

// MyApp은 Kubernetes API에 등록될 Custom Resource 타입이다.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas"
type MyApp struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    // 사용자가 입력하는 desired state
    Spec   MyAppSpec   `json:"spec,omitempty"`

    // Controller가 업데이트하는 observed state
    Status MyAppStatus `json:"status,omitempty"`
}
```

여기서 역할이 분리된다.


| 필드       | 누가 쓰나      | 의미        |
| -------- | ---------- | --------- |
| `spec`   | 사용자/GitOps | 원하는 상태    |
| `status` | Controller | 관찰된 현재 상태 |


`+kubebuilder:validation`, `+kubebuilder:subresource:status` 같은 주석은 `controller-gen`이 CRD YAML을 만들 때 사용하는 marker다.

`controller-gen`은 Kubebuilder 프로젝트에서 Go 코드와 marker 주석을 읽어 Kubernetes 매니페스트와 자동 생성 코드를 만들어 주는 도구다. 개발자가 긴 CRD YAML이나 RBAC YAML을 직접 작성하지 않도록 도와준다.

```text
api/.../myapp_types.go
  -> controller-gen
  -> config/crd/bases/...yaml

controller RBAC marker
  -> controller-gen
  -> config/rbac/role.yaml

runtime.Object 타입
  -> controller-gen
  -> zz_generated.deepcopy.go
```

보통 직접 `controller-gen` 명령을 길게 실행하기보다 Makefile 명령을 사용한다.

```bash
make generate   # DeepCopy 코드 생성
make manifests  # CRD/RBAC 매니페스트 생성
```

---

### 3-3. 자동 생성 파일의 의미

`groupversion_info.go`는 `MyApp` 같은 커스텀 타입을 Scheme에 등록할 수 있게 해준다.

`zz_generated.deepcopy.go`는 Kubernetes 객체를 안전하게 복사하기 위한 코드다. 자동 생성 파일이므로 직접 수정하지 않는다.

Reconcile에서 객체를 수정할 때는 보통 복사본을 만든 뒤 업데이트한다.

```go
// Cache에서 읽은 객체를 바로 바꾸지 않고 복사한다.
copy := myApp.DeepCopy()

// 관찰한 현재 상태를 status에 반영한다.
copy.Status.ReadyReplicas = 3

// status 서브리소스만 API Server에 업데이트한다.
err := r.Status().Update(ctx, copy)
```

이유는 Cache에서 읽은 객체를 직접 수정하면 내부 캐시를 오염시킬 수 있기 때문이다.

---

## 4. controller-runtime 핵심 컴포넌트

controller-runtime은 Operator 실행에 필요한 구성요소를 몇 가지 개념으로 정리한다.

Week2에서 직접 만들었던 Controller를 떠올리면, 이벤트를 감지하는 Informer, 작업을 쌓아 두는 Queue, 실제 조정 로직을 실행하는 Reconcile 함수가 따로 있었다. `controller-runtime`은 이 구조를 `Manager`, `Controller`, `Client`, `Reconciler` 같은 이름으로 묶어서 제공한다. 그래서 개발자는 “이벤트를 어떻게 Queue에 넣을까?”보다 “현재 상태를 원하는 상태로 어떻게 맞출까?”에 집중할 수 있다.

```
main.go
  → Manager 생성
  → Reconciler를 Controller에 등록
  → Manager.Start()
  → Watch 이벤트가 Reconcile 호출
```

```text
API Server
   |
   | Watch
   v
Controller -- Request(namespace/name) --> Queue
                                             |
                                             v
                                   Reconciler.Reconcile()
                                             |
                                             v
                                  Client Get/Create/Update
```

---

### 4-1. Manager

`Manager`는 Operator 프로세스의 최상위 실행 단위다.

실습에서 `make run`을 실행하면 실제로는 `cmd/main.go`가 실행되고, 그 안에서 Manager가 만들어진다. Manager는 혼자 비즈니스 로직을 수행하지는 않지만, Cache, Client, Controller 같은 부품을 시작하고 종료하는 역할을 한다. 그래서 Operator 프로세스의 “실행 관리자”라고 생각하면 된다.

Manager가 관리하는 것들은 다음과 같다.


| 항목             | 역할                                   |
| -------------- | ------------------------------------ |
| Cache          | API Server를 List/Watch해서 객체를 메모리에 보관 |
| Client         | Reconciler가 Kubernetes API를 읽고 쓰는 통로 |
| Scheme         | Go 타입과 Kubernetes GVK를 연결            |
| Controller     | Watch, Queue, Reconciler 호출 관리       |
| Metrics/Health | 운영용 메트릭과 헬스 체크 제공                    |


`mgr.Start()`가 호출되면 Cache와 Controller가 함께 실행된다.

---

### 4-2. Controller

`Controller`는 Watch 이벤트를 받아 Reconciler를 호출한다.

```go
func (r *MyAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&appsv1alpha1.MyApp{}).
        Owns(&appsv1.Deployment{}).
        Complete(r)
}
```


| 메서드          | 의미                           |
| ------------ | ---------------------------- |
| `For()`      | 주 리소스인 CR을 Watch한다           |
| `Owns()`     | CR이 소유한 하위 리소스를 Watch한다      |
| `Complete()` | Reconciler를 Controller에 연결한다 |


예를 들어 `MyApp`이 바뀌어도 Reconcile이 실행되고, `MyApp`이 만든 Deployment가 바뀌어도 Reconcile이 실행된다.

---

### 4-3. Client

`Client`는 Reconcile 안에서 Kubernetes 리소스를 조회하고 수정할 때 사용한다.

```go
err := r.Get(ctx, req.NamespacedName, &myApp)
err = r.Create(ctx, deployment)
err = r.Update(ctx, deployment)
err = r.Status().Update(ctx, &myApp)
```

기본 동작은 다음처럼 이해하면 충분하다.

```
Get/List
  → Cache에서 읽음

Create/Update/Patch/Delete/Status().Update
  → API Server에 직접 요청
```

읽기는 빠르게, 쓰기는 API Server를 통해 확정적으로 처리하는 구조다.

---

### 4-4. Reconciler

`Reconciler`는 개발자가 직접 구현하는 핵심 로직이다.

Operator를 공부할 때 가장 오래 봐야 하는 함수가 `Reconcile()`이다. Watch 이벤트가 들어오면 Controller가 바로 객체를 수정하는 것이 아니라, `namespace/name`만 담긴 요청을 Reconcile에 넘긴다. Reconcile은 그 이름으로 현재 객체를 다시 조회하고, 원하는 상태와 비교한 뒤 필요한 작업만 수행한다.

```go
func (r *MyAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // desired/current 비교 후 필요한 작업 수행
    return ctrl.Result{}, nil
}
```

표준 흐름은 다음과 같다.

```
1. CR 조회
2. CR이 없으면 종료
3. 하위 리소스 조회
4. 없으면 생성, 다르면 수정
5. status 업데이트
6. 필요하면 재시도 또는 주기적 Requeue
```

이때 중요한 원칙은 Week2와 같다.


| 원칙              | 의미                                       |
| --------------- | ---------------------------------------- |
| Level-triggered | 이벤트 종류보다 현재 상태를 기준으로 판단한다                |
| Idempotent      | 같은 Reconcile이 여러 번 실행되어도 결과가 안정적이어야 한다   |
| Status 분리       | 사용자가 쓰는 spec과 Controller가 쓰는 status를 나눈다 |


---

### 4-5. Scheme

`Scheme`은 Go 타입과 Kubernetes API 타입을 연결하는 레지스트리다.

```
*appsv1.Deployment  ↔  apps/v1/Deployment
*corev1.Pod         ↔  /v1/Pod
*MyApp              ↔  apps.example.com/v1alpha1/MyApp
```

`main.go`에서는 내장 타입과 커스텀 타입을 Scheme에 등록한다.

```go
utilruntime.Must(clientgoscheme.AddToScheme(scheme))
utilruntime.Must(appsv1alpha1.AddToScheme(scheme))
```

Scheme 등록이 빠지면 Client가 `&MyApp{}`을 어떤 API 경로로 요청해야 하는지 알 수 없다.

---

## 5. 실행 흐름: main.go에서 Reconcile까지

전체 실행 흐름은 다음 순서로 이어진다.

여기서는 세부 코드보다 “누가 누구를 연결하는가”를 보는 것이 중요하다. `main.go`는 Manager를 만들고, Reconciler를 Controller에 등록한다. Manager가 시작되면 Watch와 Queue가 동작하고, 이벤트가 들어올 때마다 Reconcile이 호출된다.

```text
main()
  ├── Scheme 초기화
  ├── Manager 생성
  ├── Reconciler 생성
  ├── SetupWithManager로 Controller 등록
  └── Manager.Start()
        ├── Cache 시작
        ├── Watch 시작
        ├── Queue 처리
        └── Reconcile 호출
```

---

### 5-1. main.go의 역할

`main.go`는 Operator 프로세스를 실행하기 위한 조립 코드다.

```go
// Operator 실행에 필요한 Manager를 만든다.
mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
    // 어떤 Kubernetes 타입을 다룰 수 있는지 알려준다.
    Scheme: scheme,
})

// Reconciler에 Client/Scheme을 주입하고 Controller에 등록한다.
err = (&controller.MyAppReconciler{
    Client: mgr.GetClient(),
    Scheme: mgr.GetScheme(),
}).SetupWithManager(mgr)

// Manager를 시작한다. 이 시점부터 Cache/Watch/Controller가 동작한다.
err = mgr.Start(ctrl.SetupSignalHandler())
```

흐름은 단순하다.

```
Manager 만들기
  → Reconciler에 Client/Scheme 주입
  → Controller 등록
  → Manager 실행
```

---

### 5-2. Reconcile 호출 흐름

Controller가 이벤트를 받으면 바로 비즈니스 로직을 실행하는 것이 아니라 Queue에 요청을 넣는다.

```text
Watch 이벤트
  → Request{namespace, name} 생성
  → Workqueue에 추가
  → Worker가 Request를 꺼냄
  → Reconcile(ctx, req) 호출
```

`Request`에는 객체 전체가 아니라 `namespace/name`만 들어 있다. 그래서 Reconcile은 항상 다시 `Get()`으로 현재 상태를 조회해야 한다.

```go
var myApp appsv1alpha1.MyApp
if err := r.Get(ctx, req.NamespacedName, &myApp); err != nil {
    return ctrl.Result{}, client.IgnoreNotFound(err)
}
```

이 방식 덕분에 이벤트가 중복되거나 순서가 조금 바뀌어도 최종 상태를 기준으로 안정적으로 동작할 수 있다.

---

### 5-3. 한 번의 Reconcile 요약

예를 들어 `MyApp` CR 하나가 Deployment 하나를 관리한다고 하면 Reconcile은 다음처럼 동작한다.

```text
1. MyApp CR 조회
2. 원하는 replicas/image 확인
3. Deployment 조회
4. Deployment가 없으면 생성
5. Deployment가 다르면 수정
6. Deployment의 readyReplicas를 MyApp status에 기록
```

핵심은 아래 한 문장이다.

> Reconcile은 “이번 이벤트가 무엇인가”보다 “지금 클러스터가 원하는 상태와 같은가”를 계속 확인하는 함수다.

---

## 6. 전체 요약

Operator는 Kubernetes Controller 패턴을 애플리케이션 운영 영역으로 확장한 것이다.

처음에는 용어가 많아 복잡해 보이지만, 결국 하나의 흐름으로 이어진다. 사용자는 CR에 원하는 상태를 적고, Operator는 그 상태가 실제 클러스터에 반영되도록 계속 맞춘다. `controller-runtime`과 Kubebuilder는 이 흐름을 구현하기 위해 반복해서 필요한 구조를 미리 제공해 주는 도구다.

```
CRD
  → 사용자가 원하는 상태를 선언하는 API

Custom Controller
  → CR을 Watch하고 Reconcile 실행

controller-runtime
  → Manager, Controller, Client, Cache, Scheme으로 구현을 단순화

Reconciler
  → 개발자가 실제 운영 자동화 로직을 작성하는 곳
```

Week3에서 꼭 잡아야 할 연결은 다음이다.


| 개념                 | 연결                                 |
| ------------------ | ---------------------------------- |
| Operator           | CRD + Custom Controller            |
| Kubebuilder        | Operator 프로젝트 뼈대 생성 도구             |
| controller-runtime | Controller 구현을 쉽게 해주는 라이브러리        |
| Manager            | Operator 프로세스의 실행 관리자              |
| Controller         | Watch 이벤트를 Queue로 보내고 Reconcile 호출 |
| Client             | Reconcile에서 리소스를 읽고 쓰는 API         |
| Reconciler         | 원하는 상태와 현재 상태를 맞추는 핵심 함수           |


결국 Operator 개발에서 개발자가 가장 많이 작성하는 곳은 두 군데다.

```
api/.../types.go
  → 어떤 spec/status를 가질지 정의

internal/controller/..._controller.go
  → 그 spec을 실제 Kubernetes 리소스로 맞추는 로직 구현
```

---

## 참고 공식 문서

- [Operator Pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
- [controller-runtime](https://pkg.go.dev/sigs.k8s.io/controller-runtime)
- [Kubebuilder Book](https://book.kubebuilder.io/)
- [Operator SDK Documentation](https://sdk.operatorframework.io/docs/)
- [Operator Capability Levels](https://operatorframework.io/operator-capabilities/)

