# Week 4 - Reconcile 패턴 구현

> CRD 설계 부분은 [week4-crd-design.md](./week4-crd-design.md)에서 다룬다.
> 실습 진행 기록은 [week4-practice.md](./week4-practice.md)를 참고한다.

---

## 이 문서의 흐름

`week4-crd-design.md`에서 spec/status를 어떻게 설계할지 정했다.
이 문서는 그 위에 올라가는 **Reconcile 구현 패턴**을 다룬다.

```
구현 단계 (이 문서)
  ├─ Reconcile 구조   : fetch → compare → act → status
  ├─ 리소스 관리      : Create / Update / Delete 멱등 처리
  ├─ 상태 비교        : Desired vs Current diff
  ├─ Ownership        : OwnerReference로 GC 연동
  └─ 에러 처리         : ctrl.Result로 Requeue / Backoff
```

이 문서는 다음 순서로 읽으면 된다.

1. Reconcile 함수의 표준 흐름을 잡는다.
2. fetch / compare / act / status 네 단계를 차례로 본다.
3. CRUD를 멱등하게 다루는 패턴을 정리한다.
4. OwnerReference로 GC와 자동 enqueue를 함께 얻는다.
5. 에러를 만났을 때 어떻게 Requeue 할지 정한다.

---

## 배경: Reconcile은 “이벤트 처리”가 아니다

Week2에서 다룬 Level-triggered 원칙은 그대로 유지된다.

> 공식 문서 (controller-runtime godoc, `Reconciler`): "Reconciler is provided the Request only — it must fetch the latest state of the object from the cluster before acting."

즉, Reconcile 함수가 받는 것은 객체가 아니라 `req.NamespacedName`뿐이다.
객체를 직접 받지 않기 때문에, 다음 규칙이 자연스럽게 강제된다.

```text
1. 이벤트의 종류(ADDED/MODIFIED/DELETED)는 알 수 없다.
2. 매번 현재 상태를 다시 조회해야 한다.
3. 같은 입력이 들어와도 같은 결과가 나와야 한다 (멱등성).
```

이 세 가지가 무너지면 Reconcile은 “이벤트 콜백”으로 퇴화한다.

---

## 1. Reconcile 구조: fetch → compare → act

### 1-1. 표준 함수 흐름

controller-runtime Reconciler의 시그니처는 다음과 같다.

```go
func (r *ImageWorkerReconciler) Reconcile(
    ctx context.Context, req ctrl.Request,
) (ctrl.Result, error)
```

여기에 들어오는 값은 객체가 아니라 `req.NamespacedName`(namespace/name)뿐이다. 그래서 Reconcile은 항상 **현재 상태를 다시 조회**하는 것에서 출발한다.

표준 흐름은 다음 네 단계다.

```text
fetch   : CR과 관련 하위 리소스를 다시 읽는다
compare : desired(spec)와 current(cluster)를 비교한다
act     : 차이를 줄이는 Create/Update/Delete를 수행한다
status  : 관찰 결과를 CR.status에 기록한다
```

네 단계를 한 함수에서 모두 한다.

```go
func (r *ImageWorkerReconciler) Reconcile(
    ctx context.Context, req ctrl.Request,
) (ctrl.Result, error) {
    log := log.FromContext(ctx)

    // 1) fetch: CR 조회
    var iw appsv1alpha1.ImageWorker
    if err := r.Get(ctx, req.NamespacedName, &iw); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // 1-1) 삭제 처리 분기 (§4-3 참고)
    if !iw.DeletionTimestamp.IsZero() {
        return r.reconcileDelete(ctx, &iw)
    }

    // 2) compare + 3) act
    desiredDep := r.buildDeployment(&iw)
    if err := r.ensureDeployment(ctx, &iw, desiredDep); err != nil {
        return r.requeueOnError(ctx, &iw, err)
    }

    desiredSvc := r.buildService(&iw)
    if err := r.ensureService(ctx, &iw, desiredSvc); err != nil {
        return r.requeueOnError(ctx, &iw, err)
    }

    // 4) status 갱신
    if err := r.updateStatus(ctx, &iw); err != nil {
        log.Error(err, "status update failed")
        return ctrl.Result{}, err
    }
    return ctrl.Result{}, nil
}
```

각 단계의 책임을 분리해서 함수로 빼면(예: `buildDeployment`, `ensureDeployment`, `updateStatus`) 멱등성과 테스트 용이성이 모두 좋아진다.

---

### 1-2. fetch 단계 — “없으면 종료” 패턴

controller-runtime의 Client는 기본적으로 **Cache에서 읽는다**. 그러므로 `Get`은 빠르지만, “이미 삭제된 객체”에 대해서는 NotFound가 돌아온다.

```go
var iw appsv1alpha1.ImageWorker
if err := r.Get(ctx, req.NamespacedName, &iw); err != nil {
    return ctrl.Result{}, client.IgnoreNotFound(err)
}
```

`client.IgnoreNotFound`는 NotFound를 nil로 바꿔준다. 이미 사라진 객체에 대해서는 “할 일 없음”으로 끝내는 것이 올바른 동작이다.

그 다음으로 캐시에서 읽은 객체를 그대로 수정하지 않는 것이 원칙이다.

```go
// Cache에서 읽은 포인터를 직접 수정하면 캐시가 오염될 수 있음
copy := iw.DeepCopy()
copy.Status.Phase = "Progressing"
err := r.Status().Update(ctx, copy)
```

> 공식 문서 (controller-runtime godoc, `client.Client`): "Objects returned from the cache are pointers into a shared store. Callers must not modify them directly."

---

### 1-3. compare 단계 — desired를 함수로 만든다

“현재 상태”와 비교할 “원하는 상태”는 **CR과 환경 정보만으로 계산되는 순수 함수**로 만드는 것이 좋다.

```go
func (r *ImageWorkerReconciler) buildDeployment(
    iw *appsv1alpha1.ImageWorker,
) *appsv1.Deployment {
    labels := map[string]string{
        "app.kubernetes.io/name":       "image-worker",
        "app.kubernetes.io/instance":   iw.Name,
        "app.kubernetes.io/managed-by": "image-worker-operator",
    }
    return &appsv1.Deployment{
        ObjectMeta: metav1.ObjectMeta{
            Name:      iw.Name,
            Namespace: iw.Namespace,
            Labels:    labels,
        },
        Spec: appsv1.DeploymentSpec{
            Replicas: ptr.To(iw.Spec.Replicas),
            Selector: &metav1.LabelSelector{MatchLabels: labels},
            Template: corev1.PodTemplateSpec{
                ObjectMeta: metav1.ObjectMeta{Labels: labels},
                Spec: corev1.PodSpec{
                    Containers: []corev1.Container{{
                        Name:  "worker",
                        Image: iw.Spec.Image,
                    }},
                },
            },
        },
    }
}
```

이렇게 `buildXxx` 함수를 만들어 두면:

- 테스트에서 “이 CR에 대해 어떤 Deployment를 만들 것인가”만 검증할 수 있다.
- compare는 단순한 “필드 동등성” 비교로 끝난다.
- 변경에 강하다(필드 추가 시 `buildXxx`만 고치면 된다).

---

### 1-4. act 단계 — CreateOrUpdate 패턴

“없으면 만들고, 있으면 필요한 만큼만 고친다”가 act의 핵심이다.

controller-runtime은 이 패턴을 `CreateOrUpdate` 헬퍼로 제공한다.

```go
import (
    "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *ImageWorkerReconciler) ensureDeployment(
    ctx context.Context, iw *appsv1alpha1.ImageWorker, desired *appsv1.Deployment,
) error {
    op, err := controllerutil.CreateOrUpdate(ctx, r.Client, desired, func() error {
        // OwnerReference (§4 참고)
        if err := controllerutil.SetControllerReference(iw, desired, r.Scheme); err != nil {
            return err
        }
        // 이 mutate 함수 안에서 "현재 객체"를 desired로 맞춘다
        desired.Spec.Replicas = ptr.To(iw.Spec.Replicas)
        desired.Spec.Template.Spec.Containers[0].Image = iw.Spec.Image
        return nil
    })
    if err != nil {
        return err
    }
    log.FromContext(ctx).Info("ensureDeployment", "op", op) // created / updated / unchanged
    return nil
}
```

> 공식 문서 (controller-runtime godoc, `controllerutil.CreateOrUpdate`): "Creates or updates the given object in the Kubernetes cluster. The object's desired state must be reconciled with the existing state inside the passed in callback `MutateFn`. The MutateFn is called regardless of creating or updating an object."

여기서 중요한 점:

- `mutate` 함수는 **이미 클러스터에서 읽어온 객체** 위에서 실행된다. 따라서 변경할 필드만 덮어써야 한다(전체 교체 X).
- 클러스터가 자동으로 채워 주는 필드(예: `clusterIP`, `nodeName`)는 절대 덮어쓰지 않는다. 덮으면 매 Reconcile마다 “바뀐 것처럼” 보여 무한 업데이트가 생긴다.

---

### 1-5. status 업데이트 — spec과 분리

status 갱신은 별도의 호출이다. spec과 같이 보내면 안 된다.

```go
func (r *ImageWorkerReconciler) updateStatus(
    ctx context.Context, iw *appsv1alpha1.ImageWorker,
) error {
    var dep appsv1.Deployment
    if err := r.Get(ctx, client.ObjectKeyFromObject(iw), &dep); err != nil {
        return client.IgnoreNotFound(err)
    }

    iw.Status.ObservedGeneration = iw.Generation
    iw.Status.ReadyReplicas = dep.Status.ReadyReplicas

    cond := metav1.Condition{
        Type:               "Ready",
        ObservedGeneration: iw.Generation,
    }
    if dep.Status.ReadyReplicas >= iw.Spec.Replicas {
        cond.Status = metav1.ConditionTrue
        cond.Reason = "AllReplicasReady"
        cond.Message = "All worker pods are ready."
    } else {
        cond.Status = metav1.ConditionFalse
        cond.Reason = "WaitingForReplicas"
        cond.Message = "Some worker pods are not ready yet."
    }
    meta.SetStatusCondition(&iw.Status.Conditions, cond)

    return r.Status().Update(ctx, iw)
}
```

핵심 규칙:

```text
1. status는 r.Status().Update() 또는 r.Status().Patch()로만 쓴다.
   → /status 서브리소스만 갱신하여 spec과 분리한다.
2. observedGeneration을 항상 같이 적는다.
   → 외부 사용자가 "이 status가 어떤 spec에 대한 관찰인가"를 알 수 있다.
3. 같은 입력에서 항상 같은 status가 나오게 한다.
   → 이전 status를 누적하거나 토글하지 않는다.
```

---

## 2. 리소스 관리: Create / Update / Delete

### 2-1. Create-or-Update의 멱등성

Reconcile은 같은 키로 여러 번 실행될 수 있다. 그래서 모든 쓰기는 **두 번 실행해도 같은 결과**여야 한다.

```text
좋지 않은 코드
  - 항상 Create 호출 → 두 번째 호출에서 AlreadyExists
  - Get 없이 Update 호출 → 충돌 가능
  - 매번 객체 전체 교체 → 자동 채움 필드 (예: clusterIP) 손상

좋은 패턴
  1) CreateOrUpdate     : 필드 단위로 덮어쓰는 mutate 함수
  2) CreateOrPatch      : 변경된 필드만 보내는 patch
  3) Server-Side Apply  : 필드 소유권 기반의 선언적 적용
```

`CreateOrUpdate` / `CreateOrPatch`는 controller-runtime이 제공한다 (§1-4 참고).

---

### 2-2. Server-Side Apply: 다른 컨트롤러와 공존하기

여러 컨트롤러나 사용자가 동일 객체를 만질 때 “누가 어떤 필드를 소유하는가”를 명확히 해야 한다. 그 답이 **Server-Side Apply(SSA)**다.

> 공식 문서 (Kubernetes Docs, "Server-Side Apply"): "Server-Side Apply helps users and controllers manage their resources through declarative configurations. Each manager can apply only the fields they own; conflicts surface when multiple managers try to set the same field."

controller-runtime에서는 `client.Apply`를 `Patch`와 함께 사용한다.

```go
desired := &appsv1.Deployment{
    TypeMeta: metav1.TypeMeta{
        APIVersion: "apps/v1", Kind: "Deployment",
    },
    ObjectMeta: metav1.ObjectMeta{
        Name:      iw.Name,
        Namespace: iw.Namespace,
    },
    Spec: /* ... 내가 책임지는 필드만 채움 ... */,
}

err := r.Patch(
    ctx, desired,
    client.Apply,
    client.FieldOwner("image-worker-operator"),
    client.ForceOwnership,
)
```

SSA의 핵심:

- 내가 적은 필드만 내가 “소유”한다.
- 다른 소유자가 같은 필드를 적으면 충돌이 명시적으로 발생한다.
- “내가 적은 적 없는 필드”는 건드리지 않는다 (예: HPA가 관리하는 `replicas`).

학습 단계에서는 `CreateOrUpdate`로 시작하고, 다른 컨트롤러/HPA와의 공존이 필요할 때 SSA로 옮기는 것이 일반적인 진행 순서다.

---

### 2-3. 삭제 처리: GC vs Finalizer

CR이 삭제될 때는 두 가지 경로가 있다.

```text
경로 A. OwnerReference만으로 충분한 경우
  - 하위 리소스가 모두 Kubernetes 안에서 끝남
  - OwnerReference (controller=true)로 묶어 두면
    Garbage Collector가 알아서 자식을 지움

경로 B. Finalizer가 필요한 경우
  - 클러스터 바깥의 외부 자원 (AWS bucket, Vault secret 등)을 정리해야 함
  - "삭제 전에 내가 해야 할 일"이 있는 경우
```

> 공식 문서 (Kubernetes Docs, "Using Finalizers"): "Finalizers alert controllers to clean up resources the deleted object owned. ... When you tell Kubernetes to delete an object that has finalizers, the API marks the object for deletion by setting `metadata.deletionTimestamp`."

Finalizer가 필요한 경우의 표준 흐름은 §4-3에서 다룬다.

---

## 3. 상태 비교: Desired vs Current

### 3-1. 무엇을 비교할 것인가

“있는지/없는지”뿐 아니라 **필드 수준 차이**까지 봐야 한다.

```text
존재 여부
  - 없음 → Create
  - 있음 → 다음 단계로

필드 수준 비교
  - replicas, image, env, resources, labels, ...
  - 다르면 → Update / Patch
  - 같으면 → 아무 것도 하지 않음 (No-op)
```

핵심 원칙:

```text
"바뀐 필드만 바꾼다."

전체 객체를 교체하지 마라
  → spec 외에 metadata.managedFields, status,
     자동 채움 필드 (NodePort, clusterIP 등)까지 건드려
     충돌과 무한 업데이트를 만든다.
```

---

### 3-2. 비교는 mutate 함수 안에서

`CreateOrUpdate`의 mutate 함수 안에는 다음 두 가지가 들어간다.

1. OwnerReference 보장
2. 내가 관리하는 필드 덮어쓰기 (그 외에는 건드리지 않음)

```go
_, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
    if err := controllerutil.SetControllerReference(iw, dep, r.Scheme); err != nil {
        return err
    }
    dep.Spec.Replicas = ptr.To(iw.Spec.Replicas)
    if len(dep.Spec.Template.Spec.Containers) == 0 {
        dep.Spec.Template.Spec.Containers = []corev1.Container{{Name: "worker"}}
    }
    dep.Spec.Template.Spec.Containers[0].Image = iw.Spec.Image
    return nil
})
```

`CreateOrUpdate`는 내부적으로 “mutate 호출 전/후의 객체”를 비교해서 차이가 없으면 API 호출을 건너뛴다. 따라서 호출자가 직접 `reflect.DeepEqual`로 차이를 검사할 필요가 없다.

---

### 3-3. 사용자가 손댄 필드 다루기

사용자가 직접 하위 Deployment의 `replicas`를 바꿔 두면 어떻게 할까? 답은 **CRD가 그 필드의 “원하는 상태”를 선언하느냐 아니냐**에 달려 있다.

```text
spec.replicas가 CRD에 정의됨
  → CRD가 진실. 항상 spec 값으로 되돌린다.

spec.replicas가 CRD에 정의되지 않음 (외부 HPA에 위임)
  → 그 필드는 절대 Reconcile에서 덮어쓰지 않는다.
  → Server-Side Apply로 "그 필드는 내가 소유하지 않음"을 명시한다.
```

이 규칙이 명확해야 사용자/HPA/Operator 사이의 “필드 줄다리기”가 생기지 않는다.

---

## 4. Ownership: OwnerReference로 GC 연동

### 4-1. OwnerReference는 무엇을 보장하는가

> 공식 문서 (Kubernetes Docs, "Owners and dependents"): "An owner reference tells Kubernetes which object is the 'owner' of another object. ... The garbage collector uses owner references to determine which dependent objects can be deleted."

OwnerReference가 하는 일은 두 가지다.

```text
1. Garbage Collection
   - owner가 삭제되면 dependent도 자동 삭제 (cascading)
   - Foreground / Background / Orphan 정책 선택 가능

2. controller-runtime의 Owns() 트리거
   - dependent가 변경되면 owner의 namespace/name으로 자동 enqueue
```

같은 객체에 여러 OwnerReference가 있을 수 있지만, **`Controller: true`인 OwnerReference는 하나뿐**이어야 한다.

```go
ownerRef := metav1.OwnerReference{
    APIVersion:         "apps.example.com/v1alpha1",
    Kind:               "ImageWorker",
    Name:               iw.Name,
    UID:                iw.UID,
    Controller:         ptr.To(true),  // 단 한 명만 true
    BlockOwnerDeletion: ptr.To(true),  // foreground deletion에서 사용
}
```

---

### 4-2. `controllerutil.SetControllerReference`

직접 OwnerReference 슬라이스를 만지면 실수하기 쉽다. controller-runtime이 헬퍼를 제공한다.

```go
if err := controllerutil.SetControllerReference(iw, dep, r.Scheme); err != nil {
    return err
}
```

`SetControllerReference`는 다음을 보장한다.

- owner와 dependent가 같은 namespace인지 확인 (namespaced → cluster-scoped는 불가).
- `Controller: true`인 OwnerReference가 이미 다른 객체로 설정돼 있으면 에러를 반환.
- `BlockOwnerDeletion`을 true로 설정.

> 공식 문서 (controller-runtime godoc, `controllerutil.SetControllerReference`): "Sets owner as a Controller OwnerReference on controlled. ... Returns an error if there is already a controller for the controlled object."

이 헬퍼는 mutate 함수 안에서 호출하는 것이 안전하다(있으면 갱신, 없으면 추가).

---

### 4-3. Finalizer를 함께 쓰는 표준 패턴

외부 자원 정리가 필요할 때 OwnerReference만으로는 부족하다. Finalizer를 함께 쓴다.

> 공식 문서 (Kubebuilder Book, "Using Finalizers"): "When deleting an object, you can add a finalizer to perform any pre-delete cleanup."

표준 흐름:

```text
1) Reconcile 시작 → DeletionTimestamp 검사
2) DeletionTimestamp가 없는 경우
   - Finalizer가 없으면 추가하고 종료 (또는 진행)
3) DeletionTimestamp가 있는 경우
   - 외부 자원 정리 수행
   - 성공 시 Finalizer 제거
   - API Server가 객체 실제 삭제
```

```go
const finalizerName = "imageworker.example.com/finalizer"

func (r *ImageWorkerReconciler) Reconcile(
    ctx context.Context, req ctrl.Request,
) (ctrl.Result, error) {
    var iw appsv1alpha1.ImageWorker
    if err := r.Get(ctx, req.NamespacedName, &iw); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // 삭제 중이 아닌 경우: finalizer 보장
    if iw.DeletionTimestamp.IsZero() {
        if !controllerutil.ContainsFinalizer(&iw, finalizerName) {
            controllerutil.AddFinalizer(&iw, finalizerName)
            if err := r.Update(ctx, &iw); err != nil {
                return ctrl.Result{}, err
            }
        }
        // ... 정상 Reconcile 진행 ...
        return ctrl.Result{}, nil
    }

    // 삭제 중: 외부 자원 정리 후 finalizer 제거
    if controllerutil.ContainsFinalizer(&iw, finalizerName) {
        if err := r.cleanupExternalResources(ctx, &iw); err != nil {
            return ctrl.Result{}, err  // 자동 재시도 (지수 백오프)
        }
        controllerutil.RemoveFinalizer(&iw, finalizerName)
        if err := r.Update(ctx, &iw); err != nil {
            return ctrl.Result{}, err
        }
    }
    return ctrl.Result{}, nil
}
```

핵심 규칙:

```text
1. Finalizer 추가는 deletionTimestamp가 없을 때만 한다.
2. 외부 자원 정리는 항상 멱등이어야 한다 (이미 없으면 성공).
3. 정리 성공 후에만 Finalizer를 제거한다.
4. cleanup이 실패하면 error를 반환 → 자동 재시도 (다음 §5).
```

---

## 5. 에러 처리: Requeue 전략

### 5-1. ctrl.Result와 error의 의미

Reconcile의 반환값은 두 개다.

```go
func (r *ImageWorkerReconciler) Reconcile(
    ctx context.Context, req ctrl.Request,
) (ctrl.Result, error)

type Result struct {
    Requeue      bool          // true면 즉시 다시 큐에 넣음
    RequeueAfter time.Duration // 0이 아니면 그만큼 뒤에 다시 큐에 넣음
}
```

> 공식 문서 (controller-runtime godoc, `reconcile.Result`): "Result contains the result of a Reconciler invocation. ... If `Requeue` is true or `RequeueAfter` is greater than 0, the request will be requeued."

세 가지 경우를 명확히 구분하면 된다.

| 반환 | 의미 | 다음 동작 |
| --- | --- | --- |
| `return ctrl.Result{}, nil` | 성공, 더 할 일 없음 | 새 이벤트가 올 때까지 대기 |
| `return ctrl.Result{}, err` | 실패 | RateLimiter 기반 **지수 백오프 재시도** |
| `return ctrl.Result{RequeueAfter: 30*time.Second}, nil` | 성공, 나중에 다시 확인 | 30초 뒤 재실행 |

이 세 가지를 섞지 말아야 한다. 특히 다음 두 가지는 자주 하는 실수다.

```text
return ctrl.Result{Requeue: true}, err
  → err로도 재시도, Requeue로도 재시도 → 의미 중복.
    error를 반환하면 controller-runtime이 알아서 재시도하므로 Requeue는 false로 두는 게 일반적.

return ctrl.Result{}, nil   // 실제로는 외부 자원이 아직 준비 안 됨
  → "성공" 신호를 보내 다시 호출되지 않음 → 영영 안 끝난 채로 멈춤
    이런 경우엔 RequeueAfter로 다시 깨워야 한다.
```

---

### 5-2. 어떤 에러를 어떻게 다룰 것인가

에러는 두 종류로 나누어 다룬다.

```text
일시적 (transient) 에러
  - 네트워크 일시 오류, 다른 Controller가 동시에 같은 객체를 갱신 (Conflict)
  - 해결: error를 그대로 반환 → 지수 백오프로 자동 재시도

영구적 (terminal) 에러
  - 잘못된 spec, 사용자 권한 부족, 존재하지 않는 의존 리소스를 명시적으로 요구
  - 해결: error로 무한 재시도하지 않는다.
    status.conditions에 Reason / Message로 기록하고,
    return ctrl.Result{}, nil 로 종료한다.
    사용자가 spec을 고치면 새 이벤트로 다시 들어온다.
```

`Conflict` 같은 경우에 자주 쓰는 도움:

```go
import apierrors "k8s.io/apimachinery/pkg/api/errors"

if err := r.Status().Update(ctx, &iw); err != nil {
    if apierrors.IsConflict(err) {
        // 다른 갱신과 부딪힘 → 즉시 재시도가 자연스러움
        return ctrl.Result{Requeue: true}, nil
    }
    return ctrl.Result{}, err
}
```

---

### 5-3. RequeueAfter: “시간 기반 재확인”

이벤트만으로는 알 수 없는 상태(예: 외부 시스템 상태, TLS 인증서 만료 시각)는 **주기적으로 다시 확인**해야 한다.

```go
// 외부 API에 아직 준비 안 됐다는 응답
return ctrl.Result{RequeueAfter: 15 * time.Second}, nil

// 인증서 만료 5분 전에 다시 깨우기
remaining := time.Until(cert.NotAfter) - 5*time.Minute
return ctrl.Result{RequeueAfter: remaining}, nil
```

핵심 원칙:

```text
RequeueAfter는 "지금은 더 할 일 없음, 그러나 N초 뒤에 다시 확인하자"는 신호다.
error 반환과는 의미가 다르다.
```

---

### 5-4. 내부 RateLimiter: 지수 백오프

`controller-runtime`은 워크큐에 `RateLimiter`를 기본으로 끼워 둔다. error를 반환하면 다음 재시도까지의 간격이 점점 늘어난다.

> 공식 문서 (controller-runtime godoc, `controller.Options.RateLimiter` / `workqueue.DefaultControllerRateLimiter`): "When a Reconciler returns an error, the request is requeued with backoff. The default rate limiter combines an exponential per-item backoff with an overall token-bucket limiter."

기본 동작은 다음과 같이 이해하면 된다.

```text
첫 실패     →  몇 ms 뒤 재시도
두 번째 실패 →  더 긴 간격
...
일정 횟수 이상 → 최대 대기 시간으로 수렴 (수백 초 단위)

전체 큐 처리량은 별도의 token bucket으로 상한 (예: 초당 10건, burst 100)
```

즉, 일시적 오류는 “error만 반환”하면 controller-runtime이 알아서 폭주를 막아 준다. 직접 `time.Sleep`을 넣거나 자체 카운트로 재시도를 막을 필요가 없다.

---

### 5-5. 상태와 에러를 함께 다루는 헬퍼

대부분의 Reconcile에서는 에러를 만나도 “status에 기록은 남기고” 종료하는 것이 좋다. 그래야 사용자가 `kubectl describe`로 원인을 본다.

```go
func (r *ImageWorkerReconciler) requeueOnError(
    ctx context.Context, iw *appsv1alpha1.ImageWorker, err error,
) (ctrl.Result, error) {
    meta.SetStatusCondition(&iw.Status.Conditions, metav1.Condition{
        Type:               "Ready",
        Status:             metav1.ConditionFalse,
        Reason:             "ReconcileError",
        Message:            err.Error(),
        ObservedGeneration: iw.Generation,
    })
    if uerr := r.Status().Update(ctx, iw); uerr != nil {
        // status update가 또 실패하면 원래 에러로 재시도
        return ctrl.Result{}, err
    }
    return ctrl.Result{}, err // 지수 백오프 재시도
}
```

이 패턴이 자리 잡으면 “사용자가 볼 수 있는 상태”와 “재시도 동작”이 자연스럽게 함께 움직인다.

---

## 6. 한 번의 Reconcile, 전체 흐름

지금까지 다룬 내용을 한 그림으로 묶으면 다음과 같다.

```text
                     ┌─────────────────────────────────┐
사용자 / GitOps ───► │  ImageWorker CR (spec)          │
                     └──────────────────┬──────────────┘
                                        │  watch
                                        ▼
                     ┌─────────────────────────────────┐
                     │  controller-runtime Manager     │
                     │   ├─ Cache (List+Watch)         │
                     │   └─ Workqueue (RateLimited)    │
                     └──────────────────┬──────────────┘
                                        │ Reconcile(req)
                                        ▼
┌──────────────────────────────────────────────────────────────┐
│  Reconciler                                                  │
│                                                              │
│  1) fetch   : r.Get(CR) — Cache에서 다시 읽음                │
│               DeletionTimestamp 확인 (§4-3)                  │
│                                                              │
│  2) compare : buildDeployment / buildService …               │
│               (CR + 환경 정보로 desired 계산)                │
│                                                              │
│  3) act     : CreateOrUpdate(desired)                        │
│               - SetControllerReference (§4-2)                │
│               - 내가 책임지는 필드만 덮어쓰기 (§3-2)         │
│                                                              │
│  4) status  : observedGeneration + conditions 갱신           │
│               r.Status().Update(...) (§1-5)                  │
│                                                              │
│  5) result  : error / RequeueAfter / nil (§5)                │
└──────────────────────────────┬───────────────────────────────┘
                               │ Create / Update / Patch
                               ▼
                     ┌─────────────────────────────────┐
                     │  Deployment / Service / ...     │
                     │  (OwnerReference → ImageWorker) │
                     └──────────────────┬──────────────┘
                                        │ Owns() watch
                                        ▼
                              다시 Reconcile(req)
```

핵심 한 줄로 요약하면 다음과 같다.

> Reconcile은 “이번 이벤트가 무엇인가”를 처리하는 함수가 아니라, “지금 클러스터가 원하는 상태와 같은가”를 매번 다시 계산하는 함수다.

---

## 7. 정리

| 주제 | 핵심 정리 |
| --- | --- |
| Reconcile 구조 | `fetch → compare → act → status` 네 단계로 함수를 분해한다 |
| fetch | `r.Get` 후 NotFound는 무시. Cache 객체는 DeepCopy해서 쓴다 |
| compare | desired는 CR로부터 계산되는 순수 함수로 분리한다 |
| act | `CreateOrUpdate` 또는 Server-Side Apply로 멱등성을 보장한다 |
| status | `r.Status().Update`로 spec과 분리. `observedGeneration`을 항상 같이 기록 |
| diff | 내가 책임지는 필드만 덮어쓰고, 자동 채움 필드는 절대 건드리지 않는다 |
| Ownership | `controllerutil.SetControllerReference`로 GC와 자동 enqueue를 한 번에 얻는다 |
| 삭제 | 외부 자원이 있으면 Finalizer, 클러스터 내부면 OwnerReference로 충분 |
| 에러 처리 | error 반환 = 지수 백오프, RequeueAfter = 시간 기반 재확인, 영구 에러는 status에 기록 후 nil |

결국 개발자가 가장 많이 만지는 곳은 Week3와 같다.

```text
api/.../types.go
  → spec/status 모양 (week4-crd-design.md 참고)

internal/controller/..._controller.go
  → fetch / compare / act / status / requeue (이 문서)
```

---

## 참고 공식 문서

- [Kubernetes Docs — Controllers](https://kubernetes.io/docs/concepts/architecture/controller/)
- [Kubernetes Docs — Owners and dependents (Garbage Collection)](https://kubernetes.io/docs/concepts/architecture/garbage-collection/#owners-dependents)
- [Kubernetes Docs — Using Finalizers](https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers/)
- [Kubernetes Docs — Server-Side Apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/)
- [controller-runtime godoc](https://pkg.go.dev/sigs.k8s.io/controller-runtime)
- [controller-runtime — `reconcile.Result`](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/reconcile#Result)
- [controller-runtime — `controllerutil.CreateOrUpdate`](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/controller/controllerutil#CreateOrUpdate)
- [controller-runtime — `controllerutil.SetControllerReference`](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/controller/controllerutil#SetControllerReference)
- [Kubebuilder Book — Implementing a Controller](https://book.kubebuilder.io/cronjob-tutorial/controller-implementation.html)
- [Kubebuilder Book — Using Finalizers](https://book.kubebuilder.io/reference/using-finalizers.html)
