# Week 4 — Reconcile 루프 구현 (Business Logic)

## 학습 목표

- Reconcile 함수의 구조 이해
- 리소스 생성/조회/업데이트 로직 구현법
- Ownership 설정 (OwnerReference)

---

## Reconcile 함수의 구조

### 1. 핵심 원칙: 멱등성 (Idempotency)

Reconcile에서 가장 중요한 원칙은 **멱등성**입니다.

> Reconcile은 몇 번을 호출해도 결과가 같아야 한다.

```
Reconcile 1번째 호출 ──┐
Reconcile 2번째 호출 ──┼──→ 클러스터 상태: Desired State ✓
Reconcile 100번째 호출 ┘
```

이 원칙 때문에 리소스를 무조건 생성하는 것이 아니라, 항상 **현재 상태를 확인한 뒤** 필요한 작업만 수행해야 합니다.

### 2. Reconcile 함수 시그니처

```go
func (r *MyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 여기에 비즈니스 로직
}
```

이 시그니처는 controller-runtime이 강제하는 **계약(contract)**입니다.

#### `ctx context.Context`

```go
log := log.FromContext(ctx)
```

- K8s 컨트롤러가 종료될 때 ctx가 cancel됨
- 모든 API 호출에 이 ctx를 넘겨야 함 → 취소 신호를 전파하기 위해

#### `req ctrl.Request`

```go
type Request struct {
    NamespacedName types.NamespacedName
    // NamespacedName = Namespace + Name
}
```

**핵심 포인트:**

`req`는 "어떤 오브젝트에 변화가 생겼다"는 **힌트**만 줄 뿐, 오브젝트 자체를 주지 않습니다.

```
Watch 이벤트 발생
      │
      ▼
┌─────────────────────────┐
│  Queue에 들어가는 것:    │
│  {                      │
│    Namespace: "default" │
│    Name:      "my-app"  │  ← 이름만 들어옴
│  }                      │
└─────────────────────────┘
      │
      ▼
Reconcile(ctx, req) 호출
→ 실제 오브젝트는 함수 안에서 직접 Get해야 함
```

**왜 오브젝트 자체를 안 넘기나?**

이벤트가 Queue에서 대기하는 동안 오브젝트가 또 바뀔 수 있기 때문입니다. 항상 **최신 상태를 직접 조회**하는 것이 원칙입니다.

### 3. `ctrl.Result` — 리턴값

```go
type Result struct {
    Requeue      bool          // Deprecated: RequeueAfter 사용 권장
    RequeueAfter time.Duration
}
```

| 리턴 패턴 | 의미 |
|---|---|
| `Result{}, nil` | 성공. 다음 변경 이벤트 있을 때까지 대기 |
| `Result{RequeueAfter: 30*time.Second}, nil` | 30초 후 다시 Reconcile |
| `Result{}, err` | 에러. 지수 백오프(exponential backoff)로 재시도 |

> ⚠️ `Requeue: true` 옵션은 공식적으로 **deprecated** 되었습니다. `RequeueAfter`를 사용하세요.

### 4. Reconcile이 호출되는 전체 흐름

```
K8s API Server
      │
      │  오브젝트 변경 감지 (Create/Update/Delete)
      ▼
┌─────────────┐
│   Watcher   │  ← controller-runtime이 자동으로 Watch 설정
└─────────────┘
      │
      ▼
┌─────────────┐
│  WorkQueue  │  ← 중복 제거(deduplicate)
│             │    같은 오브젝트 이벤트가 100번 와도
│             │    Queue엔 1개만 들어감
└─────────────┘
      │
      ▼
┌─────────────┐
│  Reconcile  │  ← 우리가 작성하는 함수
└─────────────┘
```

WorkQueue는 동일한 오브젝트에 대한 이벤트를 **자동으로 중복 제거**합니다. Reconcile은 "무슨 이벤트가 몇 번 왔는지"를 신경 쓰지 않고, **지금 현재 상태를 보고 desired state로 맞추는 것**만 합니다.

### 5. Reconcile 함수 전체 뼈대

```go
func (r *MyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := log.FromContext(ctx)

    // ① 오브젝트 조회
    myApp := &appsv1.MyApp{}
    if err := r.Get(ctx, req.NamespacedName, myApp); err != nil {
        if errors.IsNotFound(err) {
            // 오브젝트가 삭제됨 → 정상, 무시
            return ctrl.Result{}, nil
        }
        // 다른 에러 → 재시도
        return ctrl.Result{}, err
    }

    // ② 비즈니스 로직
    // - 하위 리소스 생성/업데이트
    // - Status 업데이트

    // ③ 성공
    return ctrl.Result{}, nil
}
```

---

## 리소스 생성/조회/업데이트 로직 구현법

### 1. `client.Client` — 모든 조작의 시작점

```go
type MyReconciler struct {
    client.Client  // ← K8s API Server와 통신하는 클라이언트
    Scheme *runtime.Scheme
}
```

`client.Client`의 구조:

```
client.Client
├── Reader
│   ├── Get()           ← 단일 리소스 조회
│   └── List()          ← 여러 리소스 조회
├── Writer
│   ├── Create()        ← 생성
│   ├── Delete()        ← 삭제
│   ├── Update()        ← 전체 업데이트
│   ├── Patch()         ← 부분 업데이트
│   └── DeleteAllOf()   ← 조건부 전체 삭제
└── Status()            ← status 서브리소스 전용
    ├── Update()
    └── Patch()
```

### 2. ⚠️ 반드시 알아야 할 캐시 동작 원칙

기본 클라이언트는 로컬 공유 캐시에서 읽고 API 서버에 직접 씁니다. **쓰기 직후 Get이 업데이트된 리소스를 반환한다고 보장하지 않습니다.**

```
Create() 호출
    │
    ▼
API Server에 직접 씀 ✅
    │
    ▼
Get() 호출 → 로컬 캐시에서 읽음
    │
    ▼
캐시가 아직 동기화 전일 수 있음 ⚠️
→ 방금 Create한 오브젝트가 안 보일 수 있음!
```

```go
// ❌ 이렇게 하면 안 됨 — Create 직후 Get으로 검증
r.Create(ctx, newDeploy)
r.Get(ctx, ...)  // 캐시 미동기화로 못 찾을 수 있음

// ✅ Create 후 바로 리턴 — 다음 Reconcile에서 자연스럽게 처리됨
if err := r.Create(ctx, newDeploy); err != nil {
    return ctrl.Result{}, err
}
return ctrl.Result{}, nil
```

### 3. Get — 리소스 조회와 에러 분기

```go
existing := &appsv1.Deployment{}
err := r.Get(ctx, req.NamespacedName, existing)
if err != nil {
    if errors.IsNotFound(err) {
        // ① 오브젝트가 없음 → 생성하거나 이미 삭제된 것
        //    nil 리턴 → 에러가 아니므로 재시도 불필요
        return ctrl.Result{}, nil
    }
    // ② 네트워크 오류, 권한 문제 등 → 재시도 필요
    return ctrl.Result{}, err
}
```

`IsNotFound`를 `nil`로 리턴하는 이유는 삭제된 오브젝트에 대한 Reconcile은 **정상 종료**이기 때문입니다. 에러로 처리하면 불필요한 재시도가 계속 발생합니다.

### 4. "Get → IsNotFound → Create" 기본 패턴

```go
func (r *MyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := log.FromContext(ctx)

    // ① 내 CR(커스텀 리소스) 조회
    myApp := &myappv1.MyApp{}
    if err := r.Get(ctx, req.NamespacedName, myApp); err != nil {
        if errors.IsNotFound(err) {
            return ctrl.Result{}, nil
        }
        return ctrl.Result{}, err
    }

    // ② 관리해야 할 Deployment가 이미 있는지 확인
    deployment := &appsv1.Deployment{}
    err := r.Get(ctx, types.NamespacedName{
        Name:      myApp.Name,
        Namespace: myApp.Namespace,
    }, deployment)

    if errors.IsNotFound(err) {
        // ③ 없으면 → 생성
        newDeploy := r.buildDeployment(myApp)
        if err := r.Create(ctx, newDeploy); err != nil {
            log.Error(err, "Deployment 생성 실패")
            return ctrl.Result{}, err
        }
        return ctrl.Result{}, nil
    }

    if err != nil {
        return ctrl.Result{}, err
    }

    // ④ 이미 있으면 → Update 로직
    return ctrl.Result{}, nil
}
```

흐름 정리:

```
Reconcile 시작
      │
      ▼
  CR Get()
      │
  ┌───┴───┐
  │ Not   │ → nil 리턴 (삭제된 것, 정상 종료)
  │ Found │
  └───────┘
      │ 존재함
      ▼
Deployment Get()
      │
  ┌───┴──────────────┐
  │ NotFound         │ 다른 에러
  │ → Create()       │ → err 리턴 (재시도)
  └──────────────────┘
      │ 존재함
      ▼
  Update 로직
```

### 5. 공식 헬퍼: `controllerutil.CreateOrUpdate()`

위의 "Get → IsNotFound → Create" 패턴을 공식적으로 추상화한 함수입니다. **실제 현업에서 가장 많이 쓰이는 방식**입니다.

```go
func CreateOrUpdate(
    ctx context.Context,
    c client.Client,
    obj client.Object,
    f MutateFn,          // ← desired state를 정의하는 함수
) (OperationResult, error)
```

`OperationResult` 반환값:

```
"unchanged" → 변경 없음
"created"   → 새로 생성됨
"updated"   → 기존 리소스 업데이트됨
```

공식 예제 코드:

```go
deploy := &appsv1.Deployment{
    ObjectMeta: metav1.ObjectMeta{
        Name:      "foo",
        Namespace: "default",
    },
}

op, err := controllerutil.CreateOrUpdate(context.TODO(), c, deploy, func() error {
    // 새로 생성되는 경우에만 설정해야 하는 immutable 필드
    if deploy.ObjectMeta.CreationTimestamp.IsZero() {
        deploy.Spec.Selector = &metav1.LabelSelector{
            MatchLabels: map[string]string{"foo": "bar"},
        }
    }

    // Desired state 설정 (항상 실행)
    deploy.Spec.Template = corev1.PodTemplateSpec{
        ObjectMeta: metav1.ObjectMeta{
            Labels: map[string]string{"foo": "bar"},
        },
        Spec: corev1.PodSpec{
            Containers: []corev1.Container{
                {Name: "busybox", Image: "busybox"},
            },
        },
    }
    return nil
})

if err != nil {
    log.Error(err, "Deployment reconcile 실패")
} else {
    log.Info("Deployment reconcile 성공", "operation", op)
}
```

`CreateOrUpdate` 내부 동작:

```
CreateOrUpdate() 호출
        │
        ▼
   내부에서 Get() 실행
        │
   ┌────┴─────────────┐
   │ NotFound         │ 존재함
   ▼                  ▼
MutateFn 호출      MutateFn 호출
   │                  │
   ▼                  ▼
Create() 실행      변경 있으면 Update()
   │                  │
   ▼                  ▼
"created" 반환    "updated" / "unchanged" 반환
```

#### ⚠️ `CreateOrUpdate` 공식 주의사항

`CreateOrUpdate`는 `Name/Namespace` 외에 아무 값도 설정되지 않은 상태의 `obj`를 전제로 합니다. 또한 `MutateFn`이 기본값이 있는 필드를 `nil`로 리셋하면 항상 업데이트를 수행하게 됩니다. 예를 들어, Deployment의 `Replicas`를 `nil`로 설정하면 API 서버가 기본값 `1`로 반환하기 때문에 동등성 검사가 항상 실패합니다. `MutateFn` 내에서 status 변경은 무시됩니다.

| | 직접 구현 | `CreateOrUpdate` |
|---|---|---|
| 코드 양 | 많음 | 적음 |
| 내부 동작 이해 | 직접 보임 | 추상화됨 |
| 실무 사용 | 특수 케이스 | 일반 케이스 |
| status 업데이트 | 별도 처리 | ⚠️ MutateFn 내 status 변경은 무시됨 |

### 6. `Update()` vs `Patch()` — 차이와 선택 기준

필드 A, B를 가진 오브젝트에서 C를 추가하려 할 때, 오래된 구조체로 `Update()`를 보내면 C가 사라지지만 `Patch()`는 그렇지 않습니다.

```
Update() 동작                    Patch() 동작
────────────────────             ────────────────────
내가 가진 전체 오브젝트           내가 변경한 부분만
그대로 덮어씀                     API Server에 전달

[A: 1, B: 2]  →  전송            [C: 3 추가]  →  전송

결과: [A: 1, B: 2]               결과: [A: 1, B: 2, C: 3]
← C가 없으면 C 삭제됨!            ← 기존 필드 유지됨
```

> `Update()` 전에는 반드시 `Get()`을 먼저 해야 합니다. 최신 상태를 가져온 뒤 수정하지 않으면 다른 컨트롤러가 추가한 필드가 유실됩니다.

공식 `Update` 예제:

```go
pod := &corev1.Pod{}
_ = c.Get(context.Background(), client.ObjectKey{
    Namespace: "namespace",
    Name:      "name",
}, pod)
controllerutil.AddFinalizer(pod, "new-finalizer")
_ = c.Update(context.Background(), pod)
```

#### Patch 타입 종류

```
Patch 타입
├── MergeFrom(obj)
│   → 기준 오브젝트(obj)와 현재 오브젝트의 diff만 전송
│
├── MergeFromWithOptions(obj, opts)
│   → MergeFrom + 추가 옵션 (Optimistic Lock 등)
│
├── StrategicMergeFrom(obj)
│   → K8s 내장 리소스(Deployment, Pod 등)에 특화된 Merge
│   → 배열 필드를 "교체"가 아닌 "병합"으로 처리
│
└── RawPatch(patchType, data)
    → JSON 바이트를 직접 전송
```

`MergeFrom` 사용 패턴:

```go
// ① 기준점(base) 저장
base := deployment.DeepCopy()

// ② 변경
deployment.Spec.Replicas = &newReplicas

// ③ diff만 Patch
patch := client.MergeFrom(base)
r.Patch(ctx, deployment, patch)
```

### 7. Optimistic Locking & ResourceVersion

K8s의 모든 오브젝트는 `metadata.resourceVersion` 필드를 가집니다. API Server가 오브젝트가 수정될 때마다 자동으로 값을 갱신합니다.

```
오브젝트 생성        resourceVersion: "1000"
       ↓
A가 Get()           resourceVersion: "1000" 읽음
B가 Get()           resourceVersion: "1000" 읽음
       ↓
A가 Update()        resourceVersion: "1001"로 갱신 ✅
       ↓
B가 Update() 시도   resourceVersion: "1000" 그대로 전송
       ↓
API Server          "1000은 이미 지났음" → 409 Conflict ❌
```

이것이 **낙관적 잠금(Optimistic Locking)**입니다. 잠금을 미리 걸지 않고, 충돌이 생기면 그때 에러를 냅니다.

충돌 감지를 활성화하려면 `MergeFromWithOptimisticLock`을 사용합니다:

```go
patch := client.MergeFromWithOptions(
    base,
    client.MergeFromWithOptimisticLock{},
)
r.Patch(ctx, deployment, patch)
```

충돌 에러 처리 패턴:

```go
if err := r.Patch(ctx, obj, patch); err != nil {
    if errors.IsConflict(err) {
        // 충돌 → 재시도 (Reconcile이 다시 호출되도록)
        return ctrl.Result{Requeue: true}, nil
    }
    return ctrl.Result{}, err
}
```

### 8. Status 서브리소스 — 왜 분리되어 있나

K8s API 레벨에서 `spec`과 `status`는 **완전히 분리된 엔드포인트**입니다.

```
일반 Update/Patch           Status Update/Patch
─────────────────           ───────────────────
/apis/.../deployments/foo   /apis/.../deployments/foo/status

spec 변경 가능              spec 변경 불가
status 변경 무시            status만 변경 가능
```

분리하는 이유:

```
사용자(User)          → spec만 수정할 권한
컨트롤러(Controller)  → status만 수정할 권한

→ RBAC으로 역할을 명확히 분리할 수 있음
→ spec 업데이트가 status를 덮어쓰는 사고 방지
```

Status 업데이트 시 반드시 `r.Status().Update()` 또는 `r.Status().Patch()`를 사용해야 합니다. 일반 `Update()`로 status를 수정하면 API Server가 무시합니다.

```go
// Reconcile 안에서 Status 업데이트
myApp.Status.ReadyReplicas = deployment.Status.ReadyReplicas

if err := r.Status().Update(ctx, myApp); err != nil {
    return ctrl.Result{}, err
}
```

| | `Update()` | `Patch()` | `Status().Update()` |
|---|---|---|---|
| 대상 | spec 전체 | 변경된 부분만 | status만 |
| 필드 유실 위험 | ⚠️ 있음 | 없음 | 없음 |
| Get() 선행 필요 | ✅ 필수 | ✅ 필수 | ✅ 필수 |
| 주요 사용처 | Finalizer 추가 등 | spec 부분 수정 | 컨트롤러 상태 보고 |

---

## Ownership 설정 (OwnerReference)

### 1. OwnerReference가 왜 필요한가

OwnerReference 없이 컨트롤러가 하위 리소스를 만들면 다음과 같은 문제가 생깁니다:

```
MyApp CR 삭제
      │
      ▼
MyApp은 사라졌지만...

Deployment ← 아직 살아있음 (고아 리소스)
Service    ← 아직 살아있음 (고아 리소스)
ConfigMap  ← 아직 살아있음 (고아 리소스)
```

OwnerReference를 설정하면:

```
MyApp CR 삭제
      │
      ▼
K8s Garbage Collector가 자동으로

Deployment ← 함께 삭제 ✅
Service    ← 함께 삭제 ✅
ConfigMap  ← 함께 삭제 ✅
```

### 2. `SetControllerReference` — 핵심 함수

`SetControllerReference`는 owner를 controlled의 Controller OwnerReference로 설정합니다. 이것은 controlled 오브젝트의 가비지 컬렉션과, controlled의 변경 시 owner 오브젝트를 Reconcile하기 위해 사용됩니다 (Watch + EnqueueRequestForOwner와 함께). **하나의 OwnerReference만 controller가 될 수 있기 때문에**, 이미 Controller 플래그가 설정된 다른 OwnerReference가 있으면 에러를 반환합니다.

```go
func SetControllerReference(
    owner      metav1.Object,      // 부모 (예: MyApp CR)
    controlled metav1.Object,      // 자식 (예: Deployment)
    scheme     *runtime.Scheme,
    opts       ...OwnerReferenceOption,
) error
```

실제 사용 패턴:

```go
func (r *MyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

    // ① 부모 CR 조회
    myApp := &myappv1.MyApp{}
    if err := r.Get(ctx, req.NamespacedName, myApp); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // ② 자식 Deployment ObjectMeta 설계 (Name/Namespace만 설정)
    deploy := &appsv1.Deployment{
        ObjectMeta: metav1.ObjectMeta{
            Name:      myApp.Name,
            Namespace: myApp.Namespace,
        },
    }

    // ③ OwnerReference 설정 ← 핵심 (Create 전에 반드시 호출)
    if err := controllerutil.SetControllerReference(myApp, deploy, r.Scheme); err != nil {
        return ctrl.Result{}, err
    }

    // ④ CreateOrUpdate
    op, err := controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
        deploy.Spec = buildDeploymentSpec(myApp)
        return nil
    })
    if err != nil {
        return ctrl.Result{}, err
    }
    log.Info("Deployment reconciled", "operation", op)

    return ctrl.Result{}, nil
}
```

> ⚠️ `SetControllerReference`는 반드시 `Create` / `CreateOrUpdate` **전에** 호출해야 합니다. OwnerReference가 오브젝트에 심어진 채로 생성되어야 하기 때문입니다.

### 3. `SetControllerReference` vs `SetOwnerReference`

`SetOwnerReference`는 owner를 controller로 지정하지 않고도 해당 오브젝트에 의존성을 가진다고 선언할 수 있습니다. 동일한 오브젝트에 대한 참조가 이미 존재하면 새로 제공된 버전으로 덮어씌워집니다.

| | `SetControllerReference` | `SetOwnerReference` |
|---|---|---|
| controller 플래그 | `true` | `false` |
| 하나의 오브젝트에 | **1개만** 가능 | 여러 개 가능 |
| Garbage Collection | ✅ 트리거 | ✅ 트리거 |
| Reconcile 트리거 | ✅ (변경 시 부모 Reconcile) | ❌ |
| 주 사용처 | 컨트롤러가 직접 관리하는 하위 리소스 | 단순 소유권 표시 |

### 4. `AlreadyOwnedError` — 반드시 처리해야 할 에러

`AlreadyOwnedError`는 controller reference를 설정하려는 오브젝트가 이미 다른 controller에 의해 소유되고 있을 때 반환됩니다.

```go
if err := controllerutil.SetControllerReference(myApp, deploy, r.Scheme); err != nil {
    var alreadyOwned *controllerutil.AlreadyOwnedError
    if errors.As(err, &alreadyOwned) {
        log.Error(err, "Deployment is already owned by another controller")
        return ctrl.Result{}, err
    }
    return ctrl.Result{}, err
}
```

### 5. `WithBlockOwnerDeletion` 옵션

`WithBlockOwnerDeletion`은 `metav1.OwnerReference`의 `BlockOwnerDeletion` 필드를 설정할 수 있게 해줍니다.

```go
controllerutil.SetControllerReference(
    myApp,
    deploy,
    r.Scheme,
    controllerutil.WithBlockOwnerDeletion(true),
)
```

```
BlockOwnerDeletion: false (기본)     BlockOwnerDeletion: true
──────────────────────               ──────────────────────
MyApp 삭제 요청                       MyApp 삭제 요청
      │                                     │
      ▼                                     ▼
MyApp 즉시 삭제                       MyApp 삭제 대기 (블로킹)
      │                                     │
      ▼                                     ▼
Deployment 백그라운드 삭제            Deployment 삭제 완료 후
                                      MyApp 삭제 완료
```

### 6. 전체 Reconcile 흐름 — 완성본

```
Reconcile 시작
      │
      ▼
① 부모 CR Get()
   └─ NotFound → nil 리턴 (정상 종료)
      │
      ▼
② 자식 오브젝트 ObjectMeta 설계
   (Name, Namespace만 설정)
      │
      ▼
③ SetControllerReference(부모, 자식, scheme)
      │
      ▼
④ CreateOrUpdate(자식, MutateFn)
   MutateFn 안에서 Spec 설정
      │
      ▼
⑤ Status 업데이트
   r.Status().Update(ctx, 부모CR)
      │
      ▼
⑥ 부모 삭제 시 → K8s GC가 자식 자동 삭제 ✅
```

---

## Week 4 전체 내용 연결 정리

```
Reconcile 함수 구조
    ↓  (멱등성 원칙 + Request는 이름만 담음 + WorkQueue 중복 제거)
리소스 조회 (Get)
    ↓  (IsNotFound 분기 처리 필수 + 캐시에서 읽음)
리소스 생성 (Create / CreateOrUpdate)
    ↓  (Create 직후 Get 금지 + MutateFn으로 desired state 정의)
리소스 업데이트 (Update / Patch)
    ↓  (Update = 전체 덮어씀, Patch = diff만 전송, ResourceVersion 충돌 주의)
Status 업데이트
    ↓  (반드시 r.Status().Update() 사용 — 일반 Update로는 무시됨)
OwnerReference 설정
    ↓  (SetControllerReference → GC + Reconcile 트리거)
부모 삭제 → 자식 자동 GC ✅
```

---

## References

- https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/reconcile
- https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client
- https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/controller/controllerutil
- https://pkg.go.dev/sigs.k8s.io/controller-runtime
- https://book.kubebuilder.io/cronjob-tutorial/controller-implementation
