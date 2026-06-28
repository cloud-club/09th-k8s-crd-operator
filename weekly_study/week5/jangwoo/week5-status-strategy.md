# Week 5 - Status 업데이트 전략

> Finalizer / GC 부분은 [week5-finalizer-gc.md](./week5-finalizer-gc.md)에서 이어진다.
> 실습 진행 기록은 [week5-practice.md](./week5-practice.md)를 참고한다.

---

## 이 문서의 흐름

Week4에서 `fetch → compare → act → status` 네 단계를 잡았다.
그중 마지막 `status` 단계는 “관찰 결과를 CR에 기록한다”까지만 다루고 넘어갔다.

Week5는 그 `status` 단계를 **운영 관점에서 다시 본다**. 단순히 `r.Status().Update()`를 호출하는 것과,
충돌·플래핑(flapping)·동시성까지 고려해서 status를 안전하게 쓰는 것은 다르기 때문이다.

```text
Week4에서 본 것
  - status는 r.Status().Update()로 쓴다
  - observedGeneration / conditions를 적는다

Week5에서 더 보는 것 (이 문서)
  ├─ /status 서브리소스가 보장하는 것
  ├─ Update vs Patch : 무엇이 다른가
  ├─ 낙관적 동시성 제어와 Conflict 처리
  ├─ Condition 갱신을 멱등하게 유지하기
  └─ status 플래핑(무한 업데이트) 막기
```

이 문서는 다음 순서로 읽으면 된다.

1. status를 왜 spec과 분리해서 써야 하는지 다시 정리한다.
2. `Update`와 `Patch`의 동작 차이를 이해한다.
3. resourceVersion 기반의 낙관적 동시성 제어와 Conflict를 다룬다.
4. Condition을 멱등하게 갱신하는 표준 패턴을 본다.
5. status가 무한히 갱신되는 플래핑을 막는 방법을 정리한다.

---

## 배경: status는 “관찰 결과”다

Week4의 spec/status 분리 원칙을 다시 가져온다.

> 공식 문서 (Kubernetes API Conventions, "spec and status"): "The `spec` field contains the desired state. The `status` field contains the observed state and is typically populated by the system."

즉 status는 **Controller가 매번 새로 계산해서 채우는 “지금 실제 상태”**다.
사용자가 적는 값이 아니고, 누적해서 쌓는 로그도 아니다.

```text
spec    : 사용자가 선언하는 desired state (Controller는 읽기만)
status  : Controller가 관찰해서 기록하는 observed state (사용자는 읽기만)
```

이 한 줄에서 Status 업데이트 전략의 거의 모든 규칙이 따라 나온다.

```text
1. status는 매번 "다시 계산"한다. 이전 값을 토글/누적하지 않는다.
   → 같은 입력이면 같은 status (멱등성)
2. status 쓰기는 spec 쓰기와 분리한다.
   → /status 서브리소스로만 쓴다
3. 실제로 바뀐 게 없으면 쓰지 않는다.
   → 불필요한 업데이트는 플래핑과 부하를 만든다
```

---

## 1. /status 서브리소스: 권한과 경로의 분리

### 1-1. 서브리소스가 보장하는 것

`+kubebuilder:subresource:status` 마커를 붙이면 CRD에 `/status` 서브리소스가 생긴다(Week4 §1-2).

> 공식 문서 (Kubernetes Docs, "CustomResourceDefinitions — Status subresource"): "When the status subresource is enabled, the `/status` subresource for the custom resource is exposed. ... Updates to the main resource ignore changes to the status stanza, and updates to the `/status` subresource ignore changes to anything other than the status stanza."

핵심은 **요청 경로가 두 개로 갈라진다**는 것이다.

```text
PUT /apis/<group>/<v>/namespaces/<ns>/myapps/<name>
  → spec / metadata 등만 반영. status는 무시된다.

PUT /apis/<group>/<v>/namespaces/<ns>/myapps/<name>/status
  → status만 반영. spec / metadata는 무시된다.
```

그래서 다음 두 호출이 명확히 다르다.

```go
// spec 쪽 경로 (status를 같이 적어도 무시됨)
r.Update(ctx, &myApp)

// status 쪽 경로 (status만 반영됨)
r.Status().Update(ctx, &myApp)
```

이 분리가 주는 실질적인 이점은 두 가지다.

```text
1. 권한 분리
   - 사용자/GitOps에게는 spec 쓰기 권한만 주고
   - status 쓰기 권한(myapps/status)은 Controller에게만 줄 수 있다

2. 책임 분리 (무한 루프 방지)
   - Controller가 status를 써도 spec generation이 오르지 않는다
   - 그래서 "Controller가 쓴 status" 때문에 다시 Reconcile이 도는 일이 없다
```

> 공식 문서 (Kubernetes API Conventions, "Metadata — generation"): "`metadata.generation` is a sequence number ... updated only by the system, and is incremented for `spec` changes (but not for `status` changes)."

즉 status 갱신은 `generation`을 올리지 않는다. 이 사실이 Week4의 `observedGeneration` 비교를 가능하게 한다.

---

### 1-2. RBAC: status는 별도 권한이다

서브리소스이기 때문에 RBAC도 별도로 적는다(Week4 실습 §2-1에서 본 marker).

```go
// +kubebuilder:rbac:groups=apps.jangwoo.dev,resources=myapps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.jangwoo.dev,resources=myapps/status,verbs=get;update;patch
```

`myapps`와 `myapps/status`가 **다른 리소스로 취급**된다는 점에 주의한다.
status를 쓰는 Operator라면 `myapps/status`에 `update;patch`가 반드시 있어야 한다.

---

## 2. Update vs Patch: 무엇이 다른가

status를 쓰는 방법은 크게 두 가지다.

```text
r.Status().Update(ctx, obj)  → "이 객체 전체로 교체해줘" (PUT)
r.Status().Patch(ctx, obj, patch) → "이 부분만 바꿔줘" (PATCH)
```

둘 다 결국 status를 바꾸지만, **전송 방식과 충돌 가능성**이 다르다.

---

### 2-1. Update: 전체 교체 + resourceVersion 검사

`Update`는 내가 들고 있는 객체 전체를 서버로 보내 교체한다. 이때 객체 안의 `metadata.resourceVersion`이 검사에 쓰인다.

```go
var myApp appsv1alpha1.MyApp
_ = r.Get(ctx, req.NamespacedName, &myApp)   // resourceVersion = 100 이라고 하자

myApp.Status.ReadyReplicas = 3
err := r.Status().Update(ctx, &myApp)
// 보낼 때 resourceVersion=100을 같이 보냄
// 서버의 현재 resourceVersion도 100이면 → 성공 (그리고 101로 오름)
// 그 사이 누가 먼저 바꿔서 서버가 101이면 → 409 Conflict
```

> 공식 문서 (Kubernetes API Conventions, "Concurrency Control and Consistency"): "Kubernetes leverages the concept of resource versions to achieve optimistic concurrency. ... If the resourceVersion no longer matches, the server rejects the update with a `409 Conflict`."

이게 **낙관적 동시성 제어(optimistic concurrency)**다. “남이 안 바꿨겠지”라고 낙관하고 보내되, 바뀌었으면 충돌로 막는다.

```text
[Get]  resourceVersion = 100  ─┐
                               │  이 사이에 다른 Reconcile/사용자가
[수정] status 일부 변경         │  같은 객체를 바꾸면 서버 rv=101
                               │
[Update] rv=100 으로 전송      ─┘
                               ▼
       서버 rv(101) != 보낸 rv(100)  →  409 Conflict
```

---

### 2-2. Patch: 바뀐 부분만 전송

`Patch`는 “원본 대비 무엇이 바뀌었는지”만 계산해서 보낸다. controller-runtime에서는 `client.MergeFrom`으로 만든다.

```go
var myApp appsv1alpha1.MyApp
_ = r.Get(ctx, req.NamespacedName, &myApp)

// 변경 전 스냅샷을 떠 둔다
base := myApp.DeepCopy()

// status 수정
myApp.Status.ReadyReplicas = 3

// base 대비 diff만 patch로 만들어 전송
err := r.Status().Patch(ctx, &myApp, client.MergeFrom(base))
```

`MergeFrom`으로 만든 기본 patch는 **resourceVersion을 강제하지 않는다**. 그래서 “내가 바꾼 필드”만 보내고, 다른 곳에서 다른 필드를 바꿨더라도 충돌 없이 합쳐진다.

```text
Update : [내 객체 전체] 를 통째로 보냄  → 충돌 가능, 덮어쓸 위험
Patch  : [내가 바꾼 부분] 만 보냄        → 다른 변경과 병합되기 쉬움
```

> 공식 문서 (controller-runtime godoc, `client.MergeFrom`): "MergeFrom creates a Patch that patches using the merge-patch strategy with the given object as base. ... It does not include the resourceVersion, so it will not fail on conflicts."

---

### 2-3. 둘 중 무엇을 쓰나

학습/실무 모두에서 일반적인 가이드는 다음과 같다.

| 상황 | 권장 | 이유 |
| --- | --- | --- |
| status 일부 필드만 갱신 | `Status().Patch(MergeFrom)` | diff만 보내 충돌이 적다 |
| status 전체를 내가 완전히 소유 | `Status().Update` | 단순하고 직관적 |
| “내가 본 그 버전”에서만 써야 함 | `Status().Patch(MergeFromWithOptimisticLock)` | resourceVersion 검사 유지 |

```text
간단한 단일 Operator (status를 Controller만 씀)
  → Update 도 충분하다. 충돌 나면 재시도(§3)하면 됨.

여러 컨트롤러/주체가 같은 객체를 동시에 만지는 환경
  → Patch(MergeFrom) 로 "내 필드만" 보내는 게 안전하다.
```

`MergeFrom`은 충돌을 피하지만, 반대로 “내가 읽은 그 시점 이후 누가 바꿨어도 그냥 덮는다”는 뜻이기도 하다. 만약 “내가 관찰한 그 버전에 대해서만 status를 쓰고 싶다”면 `client.MergeFromWithOptimisticLock`을 쓰면 resourceVersion 검사가 다시 켜진다.

```go
err := r.Status().Patch(
    ctx, &myApp,
    client.MergeFromWithOptimisticLock(base),
)
// base의 resourceVersion이 포함됨 → 그 사이 바뀌었으면 Conflict
```

---

## 3. 낙관적 동시성과 Conflict 처리

### 3-1. Conflict는 “정상적인” 신호다

`Update`나 optimistic-lock patch에서 `409 Conflict`가 나는 것은 버그가 아니다. “그 사이 누군가 먼저 바꿨다”는 정상 신호다. 대응은 단순하다.

```text
다시 Get 해서 최신 상태를 읽고,
그 위에서 다시 계산해서 쓴다.
```

Reconcile은 어차피 다시 호출될 수 있으므로, 가장 간단한 처리는 **에러를 무시하지 말고 재시도로 넘기는 것**이다.

```go
import apierrors "k8s.io/apimachinery/pkg/api/errors"

if err := r.Status().Update(ctx, &myApp); err != nil {
    if apierrors.IsConflict(err) {
        // 다른 갱신과 부딪힘 → 즉시 다시 큐에 넣어 새로 읽고 계산
        return ctrl.Result{Requeue: true}, nil
    }
    return ctrl.Result{}, err
}
```

이 패턴은 Week4 §5-2에서 이미 한 번 등장했다. Conflict는 “즉시 재시도가 자연스러운” 대표적인 경우다.

---

### 3-2. RetryOnConflict: 한 호출 안에서 재시도

매번 Reconcile 전체를 다시 도는 대신, status 쓰기만 그 자리에서 재시도하고 싶을 때는 표준 헬퍼가 있다.

```go
import "k8s.io/client-go/util/retry"

err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
    // 1) 항상 최신본을 다시 읽는다
    var fresh appsv1alpha1.MyApp
    if err := r.Get(ctx, req.NamespacedName, &fresh); err != nil {
        return err
    }
    // 2) 최신본 위에서 status를 다시 계산한다
    fresh.Status.ObservedGeneration = fresh.Generation
    fresh.Status.ReadyReplicas = readyReplicas
    // 3) 쓴다. Conflict면 RetryOnConflict가 1)부터 다시 돈다
    return r.Status().Update(ctx, &fresh)
})
```

> 공식 문서 (client-go godoc, `retry.RetryOnConflict`): "RetryOnConflict is used to make an update to a resource when you have to worry about conflicts caused by other code making unrelated updates to the resource at the same time. ... It re-runs the function if a conflict error is returned."

여기서 가장 중요한 규칙:

```text
재시도 함수 안에서는 반드시 "다시 Get" 한다.
  - Conflict가 났다는 건 내가 들고 있는 객체가 낡았다는 뜻
  - 같은 낡은 객체로 다시 Update 하면 또 Conflict (무한)
  - 매 시도마다 새로 읽어야 resourceVersion이 최신이 된다
```

---

### 3-3. 캐시 객체를 직접 수정하지 않기

Week4 §1-2에서 본 규칙이 여기서도 유효하다. `r.Get`이 돌려주는 것은 캐시의 공유 포인터일 수 있다.

> 공식 문서 (controller-runtime godoc, `client.Client`): "Objects returned from the cache are pointers into a shared store. Callers must not modify them directly."

그래서 status를 계산하기 전에 복사본을 만들거나(`DeepCopy`), Patch의 base 스냅샷을 따로 떠 두는 것이 안전하다.

```go
base := myApp.DeepCopy()   // patch base 또는 안전한 수정 대상
myApp.Status.Phase = "Ready"
_ = r.Status().Patch(ctx, &myApp, client.MergeFrom(base))
```

---

## 4. Condition 갱신을 멱등하게

### 4-1. 직접 배열을 만지지 않는다

`status.conditions[]`는 다축 상태를 표현하는 표준 구조다(Week4 §2-2). 직접 슬라이스를 append/검색하면 같은 `Type`이 중복되거나 `lastTransitionTime`이 잘못 갱신되기 쉽다. 표준 헬퍼를 쓴다.

```go
import "k8s.io/apimachinery/pkg/api/meta"

meta.SetStatusCondition(&myApp.Status.Conditions, metav1.Condition{
    Type:               "Ready",
    Status:             metav1.ConditionTrue,
    Reason:             "AllReplicasReady",
    Message:            "All replicas are ready.",
    ObservedGeneration: myApp.Generation,
})
```

`SetStatusCondition`의 동작을 정확히 알아 두면 플래핑을 피할 수 있다.

```text
SetStatusCondition 동작
  1. 같은 Type의 condition이 없으면 → 추가
  2. 있으면 → Reason/Message/ObservedGeneration 등을 갱신
  3. 단, Status(True/False/Unknown)가 "바뀌었을 때만"
     LastTransitionTime을 현재 시각으로 새로 찍는다
```

즉 `LastTransitionTime`은 **상태가 실제로 전이된 순간**만 의미하게 된다. 매 Reconcile마다 시각이 갱신되는 게 아니다.

---

### 4-2. Condition은 매번 다시 계산한다

상태 머신(Week4 §2-3)에서 정한 대로, Reconcile은 “지금 관찰된 사실”로 condition을 매번 새로 채운다. 이전 condition을 보고 토글하지 않는다.

```go
func computeReady(myApp *appsv1alpha1.MyApp, dep *appsv1.Deployment, found bool) metav1.Condition {
    cond := metav1.Condition{Type: "Ready", ObservedGeneration: myApp.Generation}
    switch {
    case !found:
        cond.Status, cond.Reason = metav1.ConditionFalse, "DeploymentMissing"
        cond.Message = "Deployment is not created yet."
    case dep.Status.ReadyReplicas >= myApp.Spec.Replicas:
        cond.Status, cond.Reason = metav1.ConditionTrue, "AllReplicasReady"
        cond.Message = "All replicas are ready."
    default:
        cond.Status, cond.Reason = metav1.ConditionFalse, "WaitingForReplicas"
        cond.Message = "Some replicas are not ready yet."
    }
    return cond
}
```

이렇게 “순수 계산 함수”로 빼 두면:

```text
- 같은 입력 → 같은 condition (멱등)
- 테스트가 쉽다 (입력만 주면 결과 검증)
- Reconcile 본문은 SetStatusCondition 한 줄로 단순해진다
```

---

## 5. status 플래핑 막기: “바뀐 게 없으면 쓰지 않는다”

### 5-1. 왜 플래핑이 문제인가

status를 “계산했으니 무조건 쓴다”로 두면, 실제 변화가 없어도 매 Reconcile마다 API 호출이 나간다. 그게 다른 watch를 깨우고, 그 watch가 또 Reconcile을 부르는 식으로 부하가 증폭될 수 있다.

```text
나쁜 흐름 (플래핑)
  Reconcile → status 무조건 Update → status watch 이벤트
            → 또 Reconcile → 또 Update → ...  (불필요한 무한 갱신)
```

원칙은 하나다.

```text
"새로 계산한 status가 기존 status와 다를 때만 쓴다."
```

---

### 5-2. 변경 여부를 비교하고 쓰기

가장 단순한 방법은 변경 전 스냅샷과 비교하는 것이다.

```go
base := myApp.DeepCopy()

// status 다시 계산
myApp.Status.ObservedGeneration = myApp.Generation
myApp.Status.ReadyReplicas = dep.Status.ReadyReplicas
meta.SetStatusCondition(&myApp.Status.Conditions, computeReady(&myApp, &dep, found))

// 바뀐 게 없으면 호출하지 않는다
if apiequality.Semantic.DeepEqual(base.Status, myApp.Status) {
    return nil
}
return r.Status().Patch(ctx, &myApp, client.MergeFrom(base))
```

`apiequality.Semantic.DeepEqual`(`k8s.io/apimachinery/pkg/api/equality`)은 Kubernetes 객체 비교에 맞춰진 비교 함수다. 일반 `reflect.DeepEqual`보다 “의미상 같음”을 잘 판단한다.

> 참고: `meta.SetStatusCondition`은 Status가 바뀌지 않으면 `LastTransitionTime`을 건드리지 않으므로, 위의 DeepEqual 비교가 “시각만 달라서 매번 다르다”로 오작동하지 않는다.

---

### 5-3. Patch base 자체가 플래핑을 줄인다

`Status().Patch(MergeFrom(base))`를 쓰면, 실제로 바뀐 필드가 없을 때 **빈 patch**가 만들어진다. controller-runtime/apiserver는 빈 patch에 대해 실질적인 변경을 만들지 않으므로, Update보다 플래핑에 강하다. 그래도 §5-2의 명시적 비교를 같이 두면 API 호출 자체를 더 확실히 줄일 수 있다.

---

## 6. 한 번의 status 갱신, 전체 흐름

지금까지의 규칙을 한 그림으로 묶으면 다음과 같다.

```text
                       Reconcile 안, act 단계 직후
                                  │
                                  ▼
        ┌───────────────────────────────────────────────┐
        │ 1) 최신본 확보                                 │
        │    base := myApp.DeepCopy()                    │
        │    (캐시 객체 직접 수정 금지, §3-3)            │
        └───────────────────────┬───────────────────────┘
                                  ▼
        ┌───────────────────────────────────────────────┐
        │ 2) status 다시 계산 (멱등, §4-2)               │
        │    - ObservedGeneration = Generation           │
        │    - ReadyReplicas = 관찰값                    │
        │    - SetStatusCondition(Ready ...)             │
        └───────────────────────┬───────────────────────┘
                                  ▼
        ┌───────────────────────────────────────────────┐
        │ 3) 바뀐 게 없으면 종료 (플래핑 방지, §5)       │
        │    if DeepEqual(base.Status, status) → return  │
        └───────────────────────┬───────────────────────┘
                                  ▼
        ┌───────────────────────────────────────────────┐
        │ 4) /status 서브리소스로만 쓰기 (§1, §2)        │
        │    r.Status().Patch(ctx, obj, MergeFrom(base)) │
        └───────────────────────┬───────────────────────┘
                                  ▼
        ┌───────────────────────────────────────────────┐
        │ 5) Conflict면 재시도 (§3)                      │
        │    IsConflict → Requeue / RetryOnConflict      │
        └───────────────────────────────────────────────┘
```

핵심 한 줄로 요약하면 다음과 같다.

> status는 “매번 다시 계산하고, 바뀐 게 있을 때만, /status 경로로, 충돌하면 다시 읽어 쓴다”.

---

## 7. 정리

| 주제 | 핵심 정리 |
| --- | --- |
| /status 서브리소스 | spec/status 경로가 분리됨. status 갱신은 `generation`을 올리지 않는다 |
| RBAC | `myapps`와 `myapps/status`는 별도 권한. status를 쓰면 `/status`에 update;patch 필요 |
| Update | 객체 전체 교체 + resourceVersion 검사 → Conflict 가능 |
| Patch(MergeFrom) | diff만 전송, resourceVersion 미포함 → 충돌에 강함 |
| MergeFromWithOptimisticLock | 충돌 검사를 다시 켜고 싶을 때 |
| Conflict 처리 | 정상 신호. 다시 Get 후 재계산. `Requeue` 또는 `RetryOnConflict` |
| Condition | `meta.SetStatusCondition`으로만 갱신. Status가 바뀔 때만 LastTransitionTime 갱신 |
| 플래핑 방지 | 새 status가 기존과 다를 때만 쓴다 (`Semantic.DeepEqual`) |

이 문서의 결과물은 Week4에서 만든 `updateStatus` 함수가 더 단단해지는 형태로 나타난다.

```text
internal/controller/myapp_controller.go
  updateStatus()
    ├─ base := DeepCopy()                 (§3-3)
    ├─ status 재계산 (멱등)                (§4)
    ├─ if DeepEqual → return               (§5)
    └─ r.Status().Patch(MergeFrom(base))   (§2)
       + Conflict 재시도                    (§3)
```

다음 문서에서는 “삭제될 때 해야 할 일”인 Finalizer와 GC를 다룬다.

---

## 참고 공식 문서

- [Kubernetes API Conventions — Spec and Status](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status)
- [Kubernetes API Conventions — Concurrency Control and Consistency (resourceVersion)](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#concurrency-control-and-consistency)
- [Kubernetes API Conventions — Metadata (generation)](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata)
- [CustomResourceDefinitions — Status subresource](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#status-subresource)
- [controller-runtime — `client.MergeFrom`](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client#MergeFrom)
- [controller-runtime — `client.Client` (Status writer)](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client#Client)
- [client-go — `retry.RetryOnConflict`](https://pkg.go.dev/k8s.io/client-go/util/retry#RetryOnConflict)
- [apimachinery — `meta.SetStatusCondition`](https://pkg.go.dev/k8s.io/apimachinery/pkg/api/meta#SetStatusCondition)
