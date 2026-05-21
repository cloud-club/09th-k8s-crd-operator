# Week3: Kubebuilder & Operator SDK

## 1. Kubebuilder & Operator SDK란?

### 1.1 간단한 설명
- **Kubebuilder**: Kubernetes API와 Controller를 쉽게 구축하기 위한 프레임워크
- **Operator SDK**: Kubebuilder를 감싸서 더 많은 기능을 제공하는 프레임워크

### 1.2 왜 쓰는가?

#### 1.2.1 반복적인 보일러플레이트 코드 자동 생성
**Kubebuilder 없이 client-go만 사용한 경우**:
```go
// 1. DeepCopy 메서드 (수백 줄)
func (in *MyResource) DeepCopy() *MyResource {
    if in == nil { return nil }
    out := new(MyResource)
    in.DeepCopyInto(out)
    return out
}

// 2. Scheme 등록
func init() {
    SchemeBuilder.Register(&MyResource{}, &MyResourceList{})
}
```

**RBAC 권한도 yaml로 수동 작성**:
```yaml
# config/rbac/role.yaml (수십 줄)
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: myresource-controller
rules:
- apiGroups: ["myapp.example.com"]
  resources: ["myresources"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

**Kubebuilder 사용 (자동 생성)**:
```go
// RBAC 권한을 설정할 때, 형식에 맞춰 주석만 추가하면 됨
//+kubebuilder:rbac:groups=myapp.example.com,resources=myresources,verbs=get;list;watch;create;update;patch;delete
```
```bash
make generate   # DeepCopy() 자동 생성
make manifests  # RBAC yaml 자동 생성
```

#### 1.2.2 Kubernetes API 개발의 복잡성 추상화
- client-go의 저수준 API 호출을 개발자가 신경쓸 필요 없음
- controller-runtime이 watch, 재시도 로직을 자동 처리

#### 1.2.3 일관된 프로젝트 구조와 패턴 제공
- 모든 Kubebuilder 프로젝트가 동일한 구조 → 팀 협업 용이
- Best practices(멱등성, exponential backoff 등)가 내장됨

### 1.3 client-go와의 관계

#### 계층 구조
```
Kubebuilder (CLI 도구 + 프로젝트 스캐폴딩)
    ↓
    프로젝트 생성 후 go.mod에 controller-runtime 의존성 추가됨
    ↓
    main.go에서 controller-runtime 라이브러리를 import해서 사용
    ↓ (내부적으로)
client-go (저수준 K8s API 호출)
```

#### controller-runtime이란?
- **독립적인 Go 라이브러리**: `sigs.k8s.io/controller-runtime`
- **프로젝트의 특정 폴더나 파일이 아님**
- `go.mod`에 `require sigs.k8s.io/controller-runtime v0.23.3` 로 기록됨
- **프로젝트에서 사용하는 곳**: `main.go`, `controllers/*_controller.go`에서 import

#### client-go vs controller-runtime 비교

**client-go만 사용 (저수준, 복잡함)**:
```go
import "k8s.io/client-go/kubernetes"

func SyncMyResource(ctx context.Context) {
    clientset, _ := kubernetes.NewForConfig(config)
    
    // 1. 수동으로 Watch 설정해야 함
    watcher, _ := clientset.AppsV1().Deployments("").Watch(ctx, metav1.ListOptions{})
    
    // 2. 이벤트 처리 루프 수동 작성
    for event := range watcher.ResultChan() {
        deployment := event.Object.(*appsv1.Deployment)
        
        // 3. 수동으로 재시도 로직 구현
        // 4. 수동으로 exponential backoff 구현
        // 5. 에러 처리, 타임아웃 등등... (복잡함!)
    }
}
```

**controller-runtime 사용 (고수준, 간결함)**:
```go
import "sigs.k8s.io/controller-runtime/pkg/controller"

type MyResourceReconciler struct {
    Client client.Client
}

// controller-runtime이 자동으로 처리해주는 것들:
// ✓ Watch 설정
// ✓ 이벤트 감지  
// ✓ 재시도 로직
// ✓ exponential backoff
// ✓ 타임아웃 관리
func (r *MyResourceReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
    var resource MyResource
    r.Client.Get(ctx, req.NamespacedName, &resource)
    
    // 비즈니스 로직만 작성하면 됨
    return reconcile.Result{}, nil
}
```

**핵심 차이**: controller-runtime은 "인프라 코드"를 자동으로 처리하고, 개발자는 **비즈니스 로직만** 작성

---

## 2. Scaffolding 프로젝트의 구조

### 2.1 `kubebuilder init`으로 생성된 디렉토리 구조
```
project/
├── api/                    # CRD 정의 (Go struct)
├── config/                 # Kubernetes manifests
│   ├── manager/           # Controller 배포 설정
│   ├── rbac/             # Role/RoleBinding
│   └── samples/          # CRD 샘플 인스턴스
├── controllers/           # Reconciliation 로직
├── hack/                  # 유틸리티 스크립트
├── Makefile              # 빌드/배포 자동화
└── main.go               # 엔트리 포인트
```

### 2.2 주요 파일의 역할
- `main.go`: Manager 초기화, Controller 등록
- `controllers/*_controller.go`: 실제 비즈니스 로직
- `api/*_types.go`: CRD Spec/Status 정의

#### `Makefile`, `make run` vs `make deploy`
- 로컬 개발 시 (make run): operator를 pod로 띄우지 않고, 로컬에서 Go 프로그램을 직접 실행한다. 이때 프로그램은 config/rbac/에 있는 권한을 쓰지 않고, 현재 내 PC의 ~/.kube/config에 로그인된 내 권한을 그대로 빌려서 클러스터를 감시한다.

- 운영 환경 배포 시 (make deploy): 실제 클러스터 안에 Operator를 pod로 띄우는 명령이다. 이때 Kustomize가 의존성 순서를 완벽하게 정렬하여 하나의 거대한 YAML 스트림으로 쿠버네티스 API 서버에 던진다.

#### `api/` 안에 정의된 Go Struct → YAML 자동 생성

CRD를 Go struct로 정의:
```go
// api/myapp/v1/myresource_types.go
type MyResourceSpec struct {
    Replicas *int32 `json:"replicas,omitempty"`
    Image    string `json:"image"`
}

type MyResource struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   MyResourceSpec   `json:"spec,omitempty"`
    Status MyResourceStatus `json:"status,omitempty"`
}
```

`make manifests` 실행 → **자동으로 CRD YAML 생성**:
```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: myresources.myapp.example.com
spec:
  names:
    kind: MyResource
    plural: myresources
  group: myapp.example.com
  scope: Namespaced
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec:
            type: object
            properties:
              replicas:
                type: integer
              image:
                type: string
```

**어떻게 가능한가?**
1. Kubebuilder의 `controller-gen` 도구가 Go struct 파싱
2. JSON 마샬링 태그(`json:"..."`) 읽음
3. Go 타입을 OpenAPI 스키마로 자동 변환
4. CRD yaml 파일 생성

**장점**: 
- YAML과 코드가 항상 동기화됨 (중복 제거)
- 타입 안정성 (struct 필드가 곧 스키마)

---

## 3. controller-runtime 라이브러리 이해하기

### 3.1 핵심 개념 (3가지)

#### 🔧 Manager
- **역할**: 여러 Controller를 관리하는 최상위 관리자
- **생성**: `main.go`에서 `ctrl.NewManager(config, opts)` 로 생성
- **책임**:
  - API server와의 연결 관리
  - Logger, Metrics 설정
  - 모든 Controller 실행/종료 제어

#### 👁️ Controller
- **역할**: 특정 리소스(예: Guestbook)의 상태 변화를 감시하고 반응
- **동작 방식**:
  1. 리소스 변화 감지 (Watch)
  2. Reconcile() 메서드 호출
  3. 현재 상태와 원하는 상태 비교
  4. 필요시 상태 변경 (Create/Update/Delete)

#### 🔄 Reconcile Loop
```
┌─────────────────────────────────────┐
│ API Server                          │
│  (리소스 변경 감지)                  │
└────────────┬──────────────────────┘
             │
             ▼
┌─────────────────────────────────────┐
│ 1. Reconcile() 호출                  │
│    req = {Name, Namespace}          │
└────────────┬──────────────────────┘
             │
             ▼
┌─────────────────────────────────────┐
│ 2. 현재 상태 조회                    │
│    client.Get(ctx, name, resource)  │
└────────────┬──────────────────────┘
             │
             ▼
┌─────────────────────────────────────┐
│ 3. 상태 변경 (Create/Update/Delete)  │
│    비즈니스 로직 실행                │
└────────────┬──────────────────────┘
             │
             ▼
┌─────────────────────────────────────┐
│ 4. 결과 반환                         │
│    Result{RequeueAfter: 10s}        │
│    또는 Error 반환                   │
└─────────────────────────────────────┘
```

### 3.2 핵심 인터페이스

#### `Reconciler` 인터페이스
```go
type Reconciler interface {
    Reconcile(context.Context, Request) (Result, error)
}
```
Reconcile은 개발자가 따로 구현해야한다.

#### `Client` 인터페이스
```go
// CRUD 작업을 수행하는 client
r.Client.Get(ctx, key, obj)           // 조회
r.Client.Create(ctx, obj)              // 생성
r.Client.Update(ctx, obj)              // 수정
r.Client.Delete(ctx, obj)              // 삭제
r.Client.List(ctx, list, opts...)     // 목록 조회
```

#### `EventRecorder` 인터페이스
```go
// 쿠버네티스 이벤트 기록 (kubectl describe pod 에서 볼 수 있음)
r.Recorder.Event(obj, "Normal", "Created", "Pod was created")
r.Recorder.Event(obj, "Warning", "Failed", "Failed to create Pod")
```

### 3.3 재시도와 백오프 전략

Reconcile() 반환값에 따른 동작:

```go
// Case 1: 성공 (다시 호출 안 함)
return ctrl.Result{}, nil

// Case 2: 일정 시간 후 재시도
return ctrl.Result{RequeueAfter: 10 * time.Second}, nil

// Case 3: 즉시 재시도 (무한 루프 주의!)
return ctrl.Result{Requeue: true}, nil

// Case 4: 에러 발생 (exponential backoff로 재시도)
return ctrl.Result{}, fmt.Errorf("failed to sync")
```

---

## 4. Quick start의 `guestbook_controller.go` - 라인별 해석

```go
package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	webappv1 "my.domain/guestbook/api/v1"  // 자신이 정의한 CRD 타입
)

// 📝 GuestbookReconciler: Reconciler 인터페이스를 구현할 struct
// 이 struct가 Reconcile() 메서드를 가져야 함
type GuestbookReconciler struct {
	client.Client                    // ✅ Kubernetes API 호출용 client
	Scheme *runtime.Scheme            // ✅ Go 타입과 K8s 타입의 매핑 정보
}

// 🔐 RBAC 권한 설정 (make manifests 실행 시 Role yaml 자동 생성)
// +kubebuilder:rbac:groups=webapp.my.domain,resources=guestbooks,verbs=get;list;watch;create;update;patch;delete
//   └─ Guestbook 리소스에 대한 CRUD 권한
// +kubebuilder:rbac:groups=webapp.my.domain,resources=guestbooks/status,verbs=get;update;patch
//   └─ Guestbook의 status 필드 수정 권한 (따로 필요!)
// +kubebuilder:rbac:groups=webapp.my.domain,resources=guestbooks/finalizers,verbs=update
//   └─ Finalizer 수정 권한 (리소스 삭제 로직에 필요)

// 🔄 Reconcile: Controller의 핵심! 여기가 "reconciliation loop"이 실행되는 곳
// 
// 호출 시점:
//   1. Guestbook 리소스가 생성될 때
//   2. Guestbook 리소스가 수정될 때
//   3. 관련 리소스(Pod, Service 등)가 변경될 때
//   4. SetupWithManager에서 설정한 재시도 간격마다
func (r *GuestbookReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// 📋 req에 포함된 정보:
	//   - req.Name: 리소스 이름 (예: "my-guestbook")
	//   - req.Namespace: 네임스페이스 (예: "default")
	
	_ = logf.FromContext(ctx)  // 로거 가져오기 (필요시 사용)

	// 💡 TODO: 여기부터 비즈니스 로직을 작성!
	// 예시:
	//   1. Guestbook 리소스 조회
	//      var guestbook webappv1.Guestbook
	//      r.Client.Get(ctx, req.NamespacedName, &guestbook)
	//
	//   2. 원하는 상태 정의 (예: Deployment 생성)
	//      deployment := &appsv1.Deployment{...}
	//
	//   3. 실제 상태와 비교하여 필요시 생성/수정/삭제
	//      if err := r.Client.Create(ctx, deployment); err != nil {
	//          return ctrl.Result{}, err
	//      }
	//
	//   4. 결과 반환
	//      return ctrl.Result{}, nil  // 성공
	//      또는
	//      return ctrl.Result{RequeueAfter: 10*time.Second}, nil  // 10초 후 재시도

	return ctrl.Result{}, nil  // 현재는 아무것도 안 함 (TODO)
}

// ⚙️ SetupWithManager: main.go에서 호출되는 초기화 메서드
// 이 메서드가 "어떤 리소스를 감시할지" 정의함
func (r *GuestbookReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&webappv1.Guestbook{}).        // 👁️ 이 리소스 변경을 감시
		Named("guestbook").                // 🏷️ Controller 이름 설정
		Complete(r)                         // ✅ Reconciler 등록 
        
        // 추가 설명: controller-runtime 라이브러리가 내부적으로 controller.New(...)라는 함수를 호출하여 진짜 Controller 객체를 메모리에 생성하고 Manager에게 등록
	
	// 체이닝 메서드의 의미:
	// 1. NewControllerManagedBy(mgr) - 이 Manager 하에 새 Controller 생성
	// 2. For(&webappv1.Guestbook{}) - "Guestbook" 리소스 변경 감시
	// 3. Owns(&appsv1.Deployment{}) - (선택) 생성한 Deployment도 함께 감시
	// 4. Watches(...) - (선택) 다른 리소스 변경도 감시
	// 5. Named("guestbook") - 로그/메트릭에서 사용할 Controller 이름
	// 6. Complete(r) - 설정 완료, Reconciler 등록
}
```

### 실제 구현 예시

```go
func (r *GuestbookReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1️⃣ Guestbook 리소스 조회
	var guestbook webappv1.Guestbook
	if err := r.Client.Get(ctx, req.NamespacedName, &guestbook); err != nil {
		log.Error(err, "unable to fetch Guestbook")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2️⃣ 원하는 상태 정의: Guestbook의 replicas 만큼 Pod를 관리하겠다
	replicas := *guestbook.Spec.Replicas
	
	// 3️⃣ 현재 상태 조회: 관련 Pod 목록 조회
	var podList corev1.PodList
	listOpts := []client.ListOption{
		client.InNamespace(req.Namespace),
		client.MatchingLabels(map[string]string{
			"guestbook.example.com/name": guestbook.Name,
		}),
	}
	if err := r.Client.List(ctx, &podList, listOpts...); err != nil {
		log.Error(err, "unable to list child Pods")
		return ctrl.Result{}, err
	}

	// 4️⃣ 비교 & 조정
	currentReplicas := int32(len(podList.Items))
	if currentReplicas < replicas {
		// Pod 부족 → 생성
		log.Info("creating Pod", "count", replicas-currentReplicas)
		for i := currentReplicas; i < replicas; i++ {
			pod := &corev1.Pod{...}
			if err := r.Client.Create(ctx, pod); err != nil {
				log.Error(err, "unable to create Pod")
				return ctrl.Result{}, err
			}
		}
	} else if currentReplicas > replicas {
		// Pod 과다 → 삭제
		log.Info("deleting Pod", "count", currentReplicas-replicas)
		for i := 0; i < int(currentReplicas-replicas); i++ {
			pod := &podList.Items[i]
			if err := r.Client.Delete(ctx, pod); err != nil {
				log.Error(err, "unable to delete Pod")
				return ctrl.Result{}, err
			}
		}
	}

	// 5️⃣ Status 업데이트
	guestbook.Status.CurrentReplicas = currentReplicas
	if err := r.Client.Status().Update(ctx, &guestbook); err != nil {
		log.Error(err, "unable to update Guestbook status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil  // 30초마다 재확인
}
```

---