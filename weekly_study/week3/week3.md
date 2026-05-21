# Kubernetes Operator 개발 정리: SDK / Kubebuilder / Scaffolding

## 1. SDK / Kubebuilder

### 1-1. SDK / Kubebuilder란?

`Operator SDK`와 `Kubebuilder`는 Kubernetes Operator 개발에 필요한 기본 프로젝트 구조와 반복 코드를 자동으로 만들어주는 도구이다.

개발자는 Kubernetes 내부 구조를 처음부터 직접 구축하지 않아도 되고, 다음과 같은 핵심 로직에 집중할 수 있다.

- 원하는 리소스가 어떤 상태가 되어야 하는지
- 실제 클러스터 상태가 현재 어떤지
- 원하는 상태와 실제 상태가 다를 때 어떻게 맞출 것인지

즉, Operator SDK와 Kubebuilder는 Kubernetes Operator 개발을 위한 기본 뼈대를 자동으로 생성해주는 도구이다.

### Kubebuilder / Operator SDK를 사용하면 자동 생성되는 것

- 프로젝트 구조
- CRD 기본 코드
- Controller 기본 코드
- RBAC 주석 기반 설정
- Webhook 기본 구조
- Dockerfile
- Makefile
- Kustomize 배포 구조

---

## 1-2. Kubebuilder

`Kubebuilder`는 Kubernetes Controller를 만들기 위한 Go 기반 프레임워크이다.

Kubebuilder를 사용하면 Kubernetes API 패턴에 맞는 Controller와 CRD 구조를 빠르게 만들 수 있다.

### Kubebuilder의 주요 특징

#### 1. 코드 생성

CRD 및 Controller 개발에 필요한 반복적인 보일러플레이트 코드를 자동으로 생성한다.

#### 2. 표준 기반

Kubebuilder는 Kubernetes API 패턴을 따르며, `controller-runtime` 라이브러리를 기반으로 구축되어 있다.

`controller-runtime`은 Kubernetes Controller 개발을 쉽게 할 수 있도록 도와주는 고수준 라이브러리이다.

#### 3. Scaffolding 지원

Kubebuilder는 프로젝트 초기 구조를 자동으로 생성한다.

예를 들어 다음과 같은 파일들을 자동으로 만들어준다.

- 구성 파일
- Dockerfile
- Makefile
- CRD 관련 디렉토리
- RBAC 관련 디렉토리
- Controller 기본 코드

#### 4. 테스트 지원

Kubebuilder는 Kubernetes용 테스트 도구인 `envtest`와의 통합을 지원한다.

이를 통해 실제 Kubernetes 클러스터 없이도 API Server, etcd와 유사한 테스트 환경에서 Controller 로직을 검증할 수 있다.

#### 5. 확장성

기본 구조가 생성된 이후에는 개발자가 직접 Spec, Status, Reconcile 로직 등을 수정하여 원하는 Operator 기능을 구현할 수 있다.

---

## 1-3. Operator SDK

`Operator SDK`는 Kubernetes에서 사용자가 직접 정의한 Custom Resource와 이를 관리하는 자동화 엔진인 Operator를 쉽고 빠르게 개발할 수 있도록 도와주는 개발 프레임워크이다.

Operator SDK는 Operator 개발에 필요한 다음 요소들을 자동으로 생성해준다.

- 프로젝트 구조
- API 타입 파일
- Controller 파일
- CRD YAML 생성 구조
- RBAC YAML 생성 구조
- Dockerfile
- Makefile
- 배포용 Kustomize 구조

Operator SDK는 Kubebuilder와 유사하게 Kubernetes Operator 개발을 쉽게 하기 위한 도구이며, 내부적으로 Kubebuilder 계열의 구조를 활용하는 경우가 많다.

---

# 2. Scaffolding

## 2-1. Scaffolding이란?

`Scaffolding`은 복잡하고 엄격한 Kubernetes 표준 규격을 실수 없이 빠르게 만들기 위해, 검증된 도구를 사용해 기본 뼈대를 자동 생성하는 작업이다.

쉽게 말하면, 개발자가 처음부터 모든 YAML 파일과 Go 코드를 직접 작성하는 것이 아니라, 도구가 기본 구조를 먼저 만들어주는 것이다.

Scaffolding은 무엇을 개발하느냐에 따라 생성되는 구조가 달라진다.

| 개발 대상 | 사용하는 도구 | 생성되는 구조 |
|---|---|---|
| Helm Chart | Helm | Kubernetes 배포 템플릿 구조 |
| Operator | Operator SDK | Operator 프로젝트 구조 |
| CRD + Controller | Kubebuilder | CRD, Controller, RBAC 구조 |

즉, Scaffolding은 개발 목적에 맞는 기본 뼈대를 자동으로 만들어주는 작업이다.

---

## 2-2. Kubernetes 확장 관점: Operator / CRD 개발

Operator SDK나 Kubebuilder에서의 Scaffolding은 Kubernetes API Server와 통신하는 Custom Controller를 만들 때 사용된다.

### 수작업으로 개발할 때의 문제점

직접 모든 구조를 작성하려면 다음과 같은 작업이 필요하다.

- Kubernetes 내부 스키마 연동
- Custom Resource 정의
- 리소스 감시 구조 설정
- Informer 설정
- Client 설정
- RBAC 권한 설정
- Controller 실행 구조 작성
- Reconcile 함수 구조 작성
- Dockerfile 작성
- 배포용 YAML 작성

이러한 작업은 반복적이고 실수하기 쉽다.

특히 Kubernetes Controller는 단순히 코드를 실행하는 프로그램이 아니라, Kubernetes API Server를 감시하고 리소스 상태를 지속적으로 맞춰야 한다.

따라서 기본 구조를 잘못 만들면 Controller가 정상적으로 동작하지 않거나, 권한 문제로 리소스를 생성하지 못하는 문제가 발생할 수 있다.

### Scaffolding을 적용하면

예를 들어 Operator SDK에서는 다음 명령어를 통해 프로젝트를 초기화할 수 있다.

```bash
operator-sdk init
```

Kubebuilder에서는 다음 명령어를 통해 프로젝트를 초기화할 수 있다.

```bash
kubebuilder init
```

이 명령어를 통해 다음과 같은 요소들이 자동 생성된다.

- CRD 매니페스트 구조
- RBAC 권한 YAML 구조
- Docker 빌드용 파일
- Controller 기본 코드
- Reconcile 함수 뼈대
- Makefile
- Kustomize 배포 구조

즉, 개발자는 Kubernetes Operator 개발에 필요한 기본 구조를 직접 만들지 않고, 실제 비즈니스 로직 구현에 집중할 수 있다.

---

## 2-3. 인프라 / 애플리케이션 배포 관점

Scaffolding은 Operator 개발에만 사용되는 개념이 아니다.

일반 애플리케이션을 Kubernetes 클러스터에 배포할 때도 사용된다.

대표적인 예시는 Helm이다.

Helm은 Kubernetes 애플리케이션 배포를 위한 패키지 매니저이다.

새로운 Helm Chart를 만들 때 다음 명령어를 사용할 수 있다.

```bash
helm create my-app
```

위 명령어를 실행하면 `my-app`이라는 디렉토리가 생성되고, 내부에 다음과 같은 파일들이 만들어진다.

```text
my-app/
├── Chart.yaml
├── values.yaml
└── templates/
    ├── deployment.yaml
    ├── service.yaml
    ├── ingress.yaml
    └── serviceaccount.yaml
```

### Helm Scaffolding 결과

| 파일 | 역할 |
|---|---|
| `Chart.yaml` | Helm Chart의 메타데이터 정의 |
| `values.yaml` | 배포 시 변경 가능한 변수 값 관리 |
| `templates/deployment.yaml` | Pod 배포 구조 정의 |
| `templates/service.yaml` | Service 구조 정의 |
| `templates/ingress.yaml` | Ingress 구조 정의 |

즉, Helm에서의 Scaffolding은 Kubernetes 애플리케이션 배포에 필요한 YAML 템플릿 구조를 자동 생성하는 것이다.

---

## 2-4. Kustomize 관점

Kustomize는 환경별 Kubernetes YAML을 관리할 때 사용된다.

예를 들어 개발 환경, 스테이징 환경, 운영 환경이 있을 경우 각각 다른 설정이 필요할 수 있다.

```text
k8s/
├── base/
│   ├── deployment.yaml
│   └── service.yaml
└── overlays/
    ├── dev/
    │   └── kustomization.yaml
    ├── staging/
    │   └── kustomization.yaml
    └── prod/
        └── kustomization.yaml
```

이런 구조를 사용하면 공통 YAML은 `base`에 두고, 환경별 차이만 `overlays`에서 관리할 수 있다.

예를 들어 개발 환경에서는 replicas를 1로 두고, 운영 환경에서는 replicas를 3으로 둘 수 있다.

---

## 2-5. Scaffolding 정리

Scaffolding은 도구별로 생성하는 대상이 다르다.

```text
Helm Chart를 만들면
→ Helm 배포 템플릿 구조 생성

Operator SDK로 만들면
→ Operator 프로젝트 구조 생성

Kubebuilder로 만들면
→ CRD + Controller 구조 생성
```

즉, Scaffolding은 “무엇을 개발하느냐”에 따라 그에 맞는 기본 뼈대를 자동으로 생성해주는 작업이다.

---

# 3. Kubebuilder에서의 Scaffolding 흐름

Kubebuilder를 사용한 Operator 개발 흐름은 다음과 같다.

```text
1. kubebuilder init
   ↓
2. 프로젝트 기본 구조 생성
   ↓
3. kubebuilder create api
   ↓
4. Custom Resource 타입 파일 생성
   ↓
5. Controller 파일 생성
   ↓
6. make manifests
   ↓
7. CRD YAML / RBAC YAML 생성
   ↓
8. make install
   ↓
9. CRD를 Kubernetes 클러스터에 등록
   ↓
10. make run 또는 make deploy
   ↓
11. Controller 실행
```

---

# 4. Kubebuilder 프로젝트 초기화

## 4-1. 프로젝트 초기화 명령어

```bash
kubebuilder init --domain example.com --repo github.com/user/my-operator
```

위 명령어를 실행하면 Kubebuilder 프로젝트의 기본 구조가 생성된다.

```text
my-operator/
├── config/
├── Dockerfile
├── Makefile
├── PROJECT
├── go.mod
├── go.sum
└── main.go
```

---

## 4-2. 주요 파일 설명

### main.go

`main.go`는 Controller가 실행될 때 시작점이 되는 파일이다.

Controller Manager를 생성하고, Scheme과 Controller를 등록한 뒤 Manager를 실행한다.

전체 흐름은 다음과 같다.

```text
main.go
↓
Manager 생성
↓
Scheme 등록
↓
Controller 등록
↓
Manager Start
```

`main.go`는 Operator 프로그램의 진입점이라고 볼 수 있다.

---

### go.mod

`go.mod`는 Go 모듈과 의존성을 관리하는 파일이다.

Kubebuilder 프로젝트에서는 보통 다음과 같은 Kubernetes 관련 라이브러리가 포함된다.

| 라이브러리 | 역할 |
|---|---|
| `sigs.k8s.io/controller-runtime` | Kubernetes Controller를 쉽게 작성할 수 있도록 도와주는 고수준 프레임워크 |
| `k8s.io/apimachinery` | Kubernetes 생태계 전반에서 사용하는 공통 유틸리티 및 데이터 모델 정의 라이브러리 |
| `k8s.io/client-go` | Kubernetes API Server와 통신하기 위한 공식 저수준 클라이언트 라이브러리 |

즉, `go.mod`는 Kubebuilder 프로젝트가 어떤 Kubernetes 라이브러리에 의존하는지를 관리하는 파일이다.

---

### Makefile

`Makefile`은 CRD 생성, 코드 생성, 빌드, 배포와 관련된 명령어를 모아둔 파일이다.

Kubebuilder 프로젝트에서는 `make` 명령어를 통해 반복 작업을 쉽게 실행할 수 있다.

### 자주 사용하는 make 명령어

| 명령어 | 설명 |
|---|---|
| `make manifests` | CRD와 RBAC 설정을 위한 YAML 파일 생성 |
| `make generate` | Go 언어용 DeepCopy 메서드 자동 생성 |
| `make install` | 현재 개발 중인 CRD만 클러스터에 적용 |
| `make run` | Controller를 로컬 환경에서 실행 |
| `make docker-build` | Controller를 컨테이너 이미지로 빌드 |
| `make deploy` | Controller를 Kubernetes 클러스터 내부에 배포 |

### 핵심 워크플로우

#### 1. 코드 변경 후 파일 갱신

```bash
make generate
make manifests
```

#### 2. 로컬에서 빠른 테스트

```bash
make install
make run
```

#### 3. 운영 환경에 가까운 배포

```bash
make docker-build IMG=<image>
make docker-push IMG=<image>
make deploy IMG=<image>
```

---

### config/

`config/` 디렉토리는 Kubernetes 배포 관련 YAML 파일이 들어가는 디렉토리이다.

일반적으로 다음과 같은 구조를 가진다.

```text
config/
├── crd/
├── rbac/
├── manager/
└── default/
```

| 디렉토리 | 역할 |
|---|---|
| `config/crd/` | CRD YAML 관련 파일 |
| `config/rbac/` | RBAC 권한 관련 YAML |
| `config/manager/` | Controller Manager 배포 관련 YAML |
| `config/default/` | 기본 배포 구성을 묶는 Kustomize 설정 |

---

# 5. API와 Controller 생성

## 5-1. Custom Resource 생성 명령어

Kubebuilder 프로젝트를 초기화한 뒤에는 내가 만들 Custom Resource를 정의한다.

예를 들어 `AppService`라는 리소스를 만든다고 가정하면 다음 명령어를 사용할 수 있다.

```bash
kubebuilder create api --group apps --version v1 --kind AppService
```

명령어를 실행하면 보통 다음과 같은 질문이 나온다.

```text
Create Resource [y/n]
Create Controller [y/n]
```

둘 다 `y`를 선택하면 Resource 타입 파일과 Controller 파일이 함께 생성된다.

---

## 5-2. 생성되는 주요 파일

```text
api/v1/appservice_types.go
internal/controller/appservice_controller.go
config/samples/apps_v1_appservice.yaml
```

| 파일 | 역할 |
|---|---|
| `api/v1/appservice_types.go` | AppService 리소스가 YAML에서 어떤 설정을 받을지 정의 |
| `internal/controller/appservice_controller.go` | AppService 리소스의 생성, 수정, 삭제를 감지하고 Reconcile 로직 수행 |
| `config/samples/apps_v1_appservice.yaml` | AppService 리소스 예시 YAML |

---

# 6. API 타입 파일 생성

## 6-1. Custom Resource 구조 정의

`api/v1/appservice_types.go` 파일은 Custom Resource의 구조를 정의하는 파일이다.

예를 들어 다음과 같이 작성할 수 있다.

```go
type AppServiceSpec struct {
    Replicas int32  `json:"replicas,omitempty"`
    Image    string `json:"image,omitempty"`
}

type AppServiceStatus struct {
    AvailableReplicas int32 `json:"availableReplicas,omitempty"`
}

type AppService struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   AppServiceSpec   `json:"spec,omitempty"`
    Status AppServiceStatus `json:"status,omitempty"`
}
```

---

## 6-2. Spec과 Status

Custom Resource는 보통 `Spec`과 `Status`를 가진다.

| 항목 | 의미 |
|---|---|
| `Spec` | 사용자가 원하는 상태 |
| `Status` | 현재 실제 실행 상태 |

예를 들어 사용자가 다음과 같은 YAML을 작성했다고 가정한다.

```yaml
apiVersion: apps.example.com/v1
kind: AppService
metadata:
  name: sample-app
spec:
  replicas: 3
  image: nginx:latest
```

여기서 `spec.replicas: 3`은 사용자가 원하는 상태이다.

즉, 사용자는 “이 애플리케이션을 nginx 이미지로 3개 실행해줘”라고 선언한 것이다.

Controller는 이 값을 보고 실제 Kubernetes 클러스터 상태를 맞춘다.

---

# 7. Controller 파일 생성

## 7-1. Controller 기본 구조

`internal/controller/appservice_controller.go` 파일에는 기본 Reconcile 함수가 생성된다.

```go
func (r *AppServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    return ctrl.Result{}, nil
}
```

처음 생성된 Reconcile 함수에는 실제 로직이 거의 없다.

개발자는 이 함수 내부에 원하는 상태와 실제 상태를 비교하고, 필요한 작업을 수행하는 로직을 작성해야 한다.

---

## 7-2. Reconcile에 작성하는 로직 예시

예를 들어 `AppService`라는 Custom Resource를 기반으로 Deployment를 생성한다고 하면 다음과 같은 흐름을 작성할 수 있다.

```text
AppService 조회
↓
Deployment 존재 여부 확인
↓
Deployment가 없으면 생성
↓
Deployment가 있으면 replicas / image 값 비교
↓
값이 다르면 Deployment 수정
↓
현재 상태를 Status에 업데이트
```

즉, Reconcile 함수는 Operator의 핵심 로직이 들어가는 부분이다.

---

# 8. RBAC 주석 생성

## 8-1. RBAC란?

RBAC는 Role-Based Access Control의 약자이다.

Kubernetes에서 어떤 주체가 어떤 리소스에 대해 어떤 작업을 할 수 있는지 제어하는 권한 관리 방식이다.

Controller가 Custom Resource를 조회하거나 수정하려면 적절한 권한이 필요하다.

예를 들어 Controller가 AppService 리소스를 조회하고 수정하려면 AppService에 대한 권한이 있어야 한다.

---

## 8-2. Kubebuilder RBAC 주석

Kubebuilder는 Controller 파일 상단에 다음과 같은 RBAC 주석을 생성한다.

```go
//+kubebuilder:rbac:groups=apps.example.com,resources=appservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps.example.com,resources=appservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps.example.com,resources=appservices/finalizers,verbs=update
```

이 주석은 단순한 설명용 주석이 아니다.

`make manifests` 명령어를 실행하면 Kubebuilder가 이 주석을 읽어서 RBAC YAML 파일을 자동으로 생성한다.

즉, 코드 주석을 기반으로 Kubernetes 권한 YAML이 만들어지는 구조이다.

---

# 9. CRD YAML 생성

## 9-1. make manifests

API 타입 파일을 수정한 뒤에는 보통 다음 명령어를 실행한다.

```bash
make manifests
```

이 명령어를 실행하면 Kubebuilder가 Go 구조체와 주석을 분석하여 CRD YAML과 RBAC YAML을 생성한다.

즉, Go 코드로 정의한 Spec과 Status가 Kubernetes API Server가 이해할 수 있는 CRD YAML로 변환된다.

---

## 9-2. Go 타입이 CRD YAML로 바뀌는 흐름

### 1. Go 타입 파일 작성

```go
type AppServiceSpec struct {
    Replicas int32  `json:"replicas,omitempty"`
    Image    string `json:"image,omitempty"`
}
```

### 2. CRD YAML 스키마 생성

위 Go 구조체를 기반으로 다음과 같은 OpenAPI 스키마가 CRD YAML에 생성된다.

```yaml
spec:
  properties:
    spec:
      properties:
        replicas:
          type: integer
          format: int32
        image:
          type: string
```

### 3. Kubernetes API Server에 등록

생성된 CRD YAML은 Kubernetes API Server에 등록된다.

등록이 완료되면 Kubernetes는 `AppService`라는 새로운 리소스 타입을 인식할 수 있다.

---

# 10. DeepCopy 코드 생성

## 10-1. DeepCopy란?

Kubernetes에서는 리소스 객체를 다룰 때 원본 객체를 직접 수정하지 않고, 복사본을 만들어 수정하는 경우가 많다.

이를 위해 Kubebuilder는 `DeepCopy` 코드를 자동으로 생성한다.

DeepCopy 코드는 다음 명령어로 생성할 수 있다.

```bash
make generate
```

명령어를 실행하면 보통 다음과 같은 파일이 생성되거나 업데이트된다.

```text
api/v1/zz_generated.deepcopy.go
```

---

## 10-2. 왜 DeepCopy가 필요한가?

Kubernetes Controller는 API Server에서 직접 객체를 매번 가져오기보다, 내부 Cache에서 객체를 가져오는 경우가 많다.

이때 Cache에서 가져온 원본 객체를 직접 수정하면 Cache 내부 상태가 꼬일 수 있다.

따라서 다음과 같은 방식으로 처리한다.

```text
원본 객체 조회
↓
DeepCopy로 복사본 생성
↓
복사본 수정
↓
API Server에 Update 또는 Patch 요청
```

즉, DeepCopy는 Kubernetes 객체를 안전하게 다루기 위한 필수 구조이다.

---

# 11. CRD를 클러스터에 설치

## 11-1. make install

CRD YAML이 생성되면 다음 명령어를 통해 CRD를 Kubernetes 클러스터에 등록할 수 있다.

```bash
make install
```

`make install`은 내부적으로 생성된 CRD YAML을 클러스터에 적용한다.

흐름은 다음과 같다.

```text
config/crd/bases/*.yaml
↓
kubectl apply
↓
Kubernetes API Server에 새로운 리소스 타입 등록
```

여기서 “내부적으로 적용한다”는 의미는 개발자가 직접 `kubectl apply -f config/crd/bases/...` 명령어를 입력하지 않아도, Makefile에 정의된 명령어가 대신 실행해준다는 뜻이다.

즉, `make install`은 CRD를 클러스터에 등록하는 자동화 명령어이다.

---

## 11-2. CRD 등록 후 확인

CRD가 등록되면 Kubernetes는 새로운 리소스 타입을 알게 된다.

이후 다음과 같은 명령어로 CRD가 등록되었는지 확인할 수 있다.

```bash
kubectl get crd
```

또는 특정 CRD를 확인할 수 있다.

```bash
kubectl get crd appservices.apps.example.com
```

CRD가 등록되었다는 것은 Kubernetes API Server가 이제 `AppService`라는 리소스 타입을 이해할 수 있다는 의미이다.

---

# 12. Sample Custom Resource 생성

## 12-1. Sample YAML 파일

Kubebuilder는 Custom Resource 예시 YAML도 함께 생성한다.

예시는 보통 다음 경로에 생성된다.

```text
config/samples/apps_v1_appservice.yaml
```

예시 파일은 다음과 같은 형태를 가진다.

```yaml
apiVersion: apps.example.com/v1
kind: AppService
metadata:
  name: appservice-sample
spec:
  replicas: 3
  image: nginx:latest
```

---

## 12-2. Sample Custom Resource 적용

다음 명령어로 Sample Custom Resource를 생성할 수 있다.

```bash
kubectl apply -f config/samples/apps_v1_appservice.yaml
```

적용 흐름은 다음과 같다.

```text
사용자 kubectl apply
↓
API Server
↓
etcd에 AppService 저장
```

이때 중요한 점은 CRD를 먼저 설치해야 Sample Custom Resource를 생성할 수 있다는 것이다.

즉, Kubernetes가 `AppService`라는 리소스 타입을 먼저 알고 있어야 한다.

---

# 13. Controller 실행

## 13-1. make run

개발 중에는 보통 다음 명령어로 Controller를 로컬 환경에서 실행한다.

```bash
make run
```

`make run`은 로컬 터미널에서 Controller를 실행하는 방식이다.

이 방식은 개발 중 빠르게 테스트할 때 많이 사용된다.

### make run 흐름

```text
로컬 PC에서 Controller 실행
↓
Controller가 Kubernetes API Server와 연결
↓
Custom Resource 변경 감지
↓
Reconcile 함수 실행
```

즉, Controller는 로컬에서 실행되지만, 실제 Kubernetes 클러스터의 API Server를 바라보며 동작한다.

---

## 13-2. make deploy

운영 환경에 가깝게 테스트하거나 실제 클러스터 내부에서 Controller를 실행하려면 Controller를 컨테이너 이미지로 빌드하고 배포해야 한다.

```bash
make docker-build IMG=<image>
make docker-push IMG=<image>
make deploy IMG=<image>
```

이 방식은 Controller 자체를 Pod로 만들어 Kubernetes 클러스터 안에서 실행하는 방식이다.

### make deploy 흐름

```text
Controller 코드 작성
↓
Docker 이미지 빌드
↓
이미지 레지스트리에 Push
↓
Kubernetes 클러스터에 Controller Manager 배포
↓
Controller가 Pod로 실행
```

---

# 14. Controller 실행 이후 흐름

Controller가 실행된 이후에는 다음과 같은 흐름으로 동작한다.

```text
Controller 실행
↓
API Server Watch 시작
↓
AppService 리소스 변경 감지
↓
Reconcile 호출
↓
현재 상태 조회
↓
원하는 상태와 실제 상태 비교
↓
필요한 리소스 생성 / 수정 / 삭제
↓
Status 업데이트
```

Controller의 핵심은 지속적으로 상태를 비교하고 맞추는 것이다.

사용자가 원하는 상태는 Custom Resource의 `Spec`에 들어있고, 실제 상태는 Kubernetes 클러스터에 존재하는 Deployment, Service, Pod 등의 상태이다.

Controller는 이 둘을 비교해서 차이가 있으면 실제 상태를 원하는 상태에 맞게 수정한다.

---

# 15. Scaffolding의 지원 범위

Kubebuilder 또는 Operator SDK의 Scaffolding은 다음과 같은 부분을 자동으로 지원한다.

- 프로젝트 구조 생성
- API 타입 파일 생성
- Controller 파일 생성
- CRD 생성 구조 준비
- RBAC 생성 구조 준비
- 배포 파일 구조 준비
- Makefile 준비
- Dockerfile 준비
- Kustomize 구조 준비

즉, 반복적이고 표준화된 부분을 자동으로 만들어준다.

---

# 16. 개발자가 직접 수행해야 하는 범위

Scaffolding이 모든 것을 자동으로 만들어주는 것은 아니다.

도구는 기본 뼈대만 만들어주고, 실제 Operator의 동작 로직은 개발자가 직접 작성해야 한다.

개발자가 직접 해야 하는 일은 다음과 같다.

- Spec 필드 설계
- Status 필드 설계
- Reconcile 로직 작성
- Deployment 생성 코드 작성
- Service 생성 코드 작성
- 리소스 수정 로직 작성
- 에러 처리
- Status 업데이트
- OwnerReference 설정
- Finalizer 처리
- 테스트 작성

즉, Scaffolding은 “기본 구조”를 만들어주는 것이고, 실제 “동작 방식”은 개발자가 구현해야 한다.

---

# 17. 전체 개발 흐름 요약

Kubebuilder 기반 Operator 개발 흐름을 정리하면 다음과 같다.

```text
1. 프로젝트 초기화
   kubebuilder init

2. API와 Controller 생성
   kubebuilder create api

3. Spec / Status 설계
   api/v1/*_types.go 수정

4. Controller 로직 작성
   internal/controller/*_controller.go 수정

5. DeepCopy 코드 생성
   make generate

6. CRD / RBAC YAML 생성
   make manifests

7. CRD 클러스터 등록
   make install

8. Sample Custom Resource 생성
   kubectl apply -f config/samples/...

9. Controller 실행
   make run 또는 make deploy

10. Reconcile 동작 확인
   kubectl get / logs / describe 등으로 확인
```
