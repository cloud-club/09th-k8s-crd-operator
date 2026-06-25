# Week 5 - 실습 (Status 전략 & Finalizer/GC)

> 이론은 [week5-status-strategy.md](./week5-status-strategy.md)와 [week5-finalizer-gc.md](./week5-finalizer-gc.md)를 먼저 읽고 진행한다.
> Week4 실습에서 만든 `MyApp` CRD 프로젝트(`~/Desktop/Study/k8s-study/`)를 그대로 이어 쓴다.

---

## 이 실습의 흐름

Week4 실습은 “`fetch → ensureDeployment → updateStatus`”까지 구현했다.
status는 `Status().Update()`로 한 번에 썼고, 삭제는 OwnerReference(GC)에만 의존했다.

Week5에서는 그 위에 **두 가지 고도화**를 올린다.

```text
Week4 결과
  - MyApp.spec → Deployment 생성/갱신
  - status.conditions / observedGeneration 갱신 (Status().Update)
  - MyApp 삭제 → Deployment cascading delete (GC)

Week5 목표
  1) Status 전략 고도화
     - Status().Update → Status().Patch(MergeFrom) 로 전환
     - "바뀐 게 없으면 안 쓰기"로 플래핑 제거
     - Conflict 재시도 확인
  2) Finalizer 추가
     - 삭제 시 "외부 자원 정리"를 흉내 내는 cleanup 구현
     - deletionTimestamp / finalizer 동작 관찰
  3) GC 정책 관찰
     - Foreground / Background / Orphan 삭제 차이 확인
```

진행 순서.

```text
1. 프로젝트 확인 (week4 결과 이어받기)
2. updateStatus를 Patch + 플래핑 방지로 고도화
3. Finalizer 추가 (AddFinalizer / cleanup / RemoveFinalizer)
4. 삭제 동작 관찰 (deletionTimestamp, finalizer 제거)
5. GC 정책 3종 비교 (foreground / background / orphan)
6. stuck terminating 재현과 복구
7. 정리
```

---

## 사전 준비

Week4에서 사용한 프로젝트를 그대로 사용한다.

```text
모듈: github.com/jangwoojung/test-operator
GVK : apps.jangwoo.dev/v1alpha1, Kind=MyApp
경로: ~/Desktop/Study/k8s-study/
```

준비 확인.

```bash
cd ~/Desktop/Study/k8s-study
kubectl get crd myapps.apps.jangwoo.dev
make run    # Week4 상태로 정상 기동되는지 먼저 확인
```

Week4의 Reconcile(`fetch → ensureDeployment → updateStatus`)이 동작하는 상태에서 시작한다.

---

## 1. Status 고도화: Update → Patch + 플래핑 방지

### 1-1. import 정리

`internal/controller/myapp_controller.go` 상단에 비교/동등성 패키지를 추가한다.

```go
import (
    "context"

    appsv1 "k8s.io/api/apps/v1"
    apierrors "k8s.io/apimachinery/pkg/api/errors"
    apiequality "k8s.io/apimachinery/pkg/api/equality"   // 추가
    "k8s.io/apimachinery/pkg/api/meta"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
    "sigs.k8s.io/controller-runtime/pkg/log"

    appsv1alpha1 "github.com/jangwoojung/test-operator/api/v1alpha1"
)
```

---

### 1-2. updateStatus 재작성

Week4의 `updateStatus`를 다음과 같이 바꾼다. 핵심 변화는 세 가지다.

1. 변경 전 스냅샷(`base`)을 뜬다.
2. status를 다시 계산한다(멱등).
3. 바뀐 게 없으면 호출하지 않고, 바뀌었으면 `Status().Patch`로 쓴다.

```go
func (r *MyAppReconciler) updateStatus(
    ctx context.Context, myApp *appsv1alpha1.MyApp,
) error {
    // (1) 변경 전 스냅샷 — patch base이자 플래핑 비교 기준
    base := myApp.DeepCopy()

    // (2) status 다시 계산 (관찰된 사실로 매번 새로)
    var dep appsv1.Deployment
    err := r.Get(ctx, client.ObjectKeyFromObject(myApp), &dep)
    if err != nil && !apierrors.IsNotFound(err) {
        return err
    }
    found := err == nil

    myApp.Status.ObservedGeneration = myApp.Generation
    if found {
        myApp.Status.ReadyReplicas = dep.Status.ReadyReplicas
    } else {
        myApp.Status.ReadyReplicas = 0
    }
    meta.SetStatusCondition(&myApp.Status.Conditions, computeReady(myApp, &dep, found))

    // (3) 바뀐 게 없으면 쓰지 않는다 (플래핑 방지)
    if apiequality.Semantic.DeepEqual(base.Status, myApp.Status) {
        return nil
    }

    // (4) /status 서브리소스로 diff만 전송
    return r.Status().Patch(ctx, myApp, client.MergeFrom(base))
}

// Ready condition을 순수 계산 함수로 분리 (멱등 / 테스트 용이)
func computeReady(
    myApp *appsv1alpha1.MyApp, dep *appsv1.Deployment, found bool,
) metav1.Condition {
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

> 이론: Patch vs Update는 [week5-status-strategy.md](./week5-status-strategy.md) §2, 플래핑 방지는 §5를 참고한다.

---

### 1-3. 플래핑이 사라졌는지 확인

같은 spec으로 가만히 두고, `make run` 로그에서 status 갱신 로그가 **계속 찍히지 않는지** 본다.

```bash
make run
# 다른 터미널에서:
kubectl apply -f config/samples/apps_v1alpha1_myapp.yaml
```

기대 동작.

```text
- CR 생성 직후: status가 한두 번 갱신됨 (Progressing → Ready로 전이)
- Ready 도달 후: 더 이상 status Patch가 나가지 않음
  → base.Status == myApp.Status 이므로 §1-2의 (3)에서 return
```

Week4(무조건 Update)에서는 Reconcile이 돌 때마다 status가 갱신될 수 있었지만, 이제는 “실제 변화가 있을 때만” 쓰인다.

---

### 1-4. Conflict 재시도 경로 유지

Reconcile 본문에서 status 쓰기 실패 시 Conflict를 즉시 재시도로 넘기는 처리는 Week4와 동일하게 유지한다.

```go
if err := r.updateStatus(ctx, &myApp); err != nil {
    if apierrors.IsConflict(err) {
        return ctrl.Result{Requeue: true}, nil   // 다시 읽고 재계산
    }
    return ctrl.Result{}, err
}
```

> 참고: `Status().Patch(MergeFrom)`는 resourceVersion을 강제하지 않아 Conflict가 잘 안 나지만, 다른 경로(`r.Update`)와 섞이면 여전히 날 수 있으므로 처리는 남겨 둔다.

---

## 2. Finalizer 추가

이제 “MyApp이 지워질 때 정리해야 할 외부 자원이 있다”고 가정한다. 실제 클라우드 대신, **로그로 정리 작업을 흉내 내는 cleanup**을 만든다.

### 2-1. finalizer 상수와 cleanup 함수

```go
const myAppFinalizer = "myapp.apps.jangwoo.dev/finalizer"

// 실제로는 S3/IAM/DNS 등 외부 자원 정리. 여기서는 로그로 흉내 낸다.
// 반드시 멱등이어야 한다 (여러 번 호출돼도 안전).
func (r *MyAppReconciler) cleanupExternalResources(
    ctx context.Context, myApp *appsv1alpha1.MyApp,
) error {
    log.FromContext(ctx).Info("cleanup external resources",
        "name", myApp.Name, "bucket", "demo-bucket-"+myApp.Name)
    // 예) if err := externalAPI.Delete(...); isNotFound(err) { return nil }
    return nil
}
```

---

### 2-2. Reconcile에 삭제 경로 분기 추가

`Reconcile` 본문을 “정상 경로 / 삭제 경로”로 나눈다. fetch 직후가 분기 지점이다.

```go
func (r *MyAppReconciler) Reconcile(
    ctx context.Context, req ctrl.Request,
) (ctrl.Result, error) {
    logger := log.FromContext(ctx)

    // fetch
    var myApp appsv1alpha1.MyApp
    if err := r.Get(ctx, req.NamespacedName, &myApp); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // ── 삭제 경로: deletionTimestamp가 찍힘 ──
    if !myApp.DeletionTimestamp.IsZero() {
        if controllerutil.ContainsFinalizer(&myApp, myAppFinalizer) {
            if err := r.cleanupExternalResources(ctx, &myApp); err != nil {
                logger.Error(err, "cleanup failed")
                return ctrl.Result{}, err   // 지수 백오프로 재시도
            }
            controllerutil.RemoveFinalizer(&myApp, myAppFinalizer)
            if err := r.Update(ctx, &myApp); err != nil {
                return ctrl.Result{}, err
            }
            logger.Info("finalizer removed; object will be deleted")
        }
        return ctrl.Result{}, nil
    }

    // ── 정상 경로: finalizer 보장 ──
    if !controllerutil.ContainsFinalizer(&myApp, myAppFinalizer) {
        controllerutil.AddFinalizer(&myApp, myAppFinalizer)
        if err := r.Update(ctx, &myApp); err != nil {
            return ctrl.Result{}, err
        }
        // finalizer를 막 추가했으니 이번 턴은 여기서 끝내고
        // 다음 Reconcile에서 이어서 처리해도 된다 (선택)
        return ctrl.Result{}, nil
    }

    // act: Deployment 보장
    if _, err := r.ensureDeployment(ctx, &myApp); err != nil {
        logger.Error(err, "ensureDeployment failed")
        return r.requeueOnError(ctx, &myApp, err)
    }

    // status 갱신 (§1)
    if err := r.updateStatus(ctx, &myApp); err != nil {
        if apierrors.IsConflict(err) {
            return ctrl.Result{Requeue: true}, nil
        }
        return ctrl.Result{}, err
    }
    return ctrl.Result{}, nil
}
```

> 이론: 삭제 경로 전체 흐름은 [week5-finalizer-gc.md](./week5-finalizer-gc.md) §3을 참고한다. finalizer는 `metadata` 변경이므로 `r.Update`(status가 아님)로 쓴다.

---

### 2-3. 실행 및 finalizer 부착 확인

```bash
make manifests
make generate
make install
make run
```

샘플 CR 적용 후 finalizer가 붙었는지 확인한다.

```bash
kubectl apply -f config/samples/apps_v1alpha1_myapp.yaml
kubectl get myapp sample-myapp -o jsonpath='{.metadata.finalizers}{"\n"}'
```

기대 출력.

```text
["myapp.apps.jangwoo.dev/finalizer"]
```

`make run` 로그.

```text
INFO ensureDeployment {"op":"created"}
```

---

## 3. 삭제 동작 관찰 (deletionTimestamp & finalizer)

### 3-1. 삭제 요청 직후 상태

삭제를 걸고, 객체가 **바로 사라지지 않고 terminating 상태로 잠깐 남는지** 본다. cleanup이 가벼워서 순식간에 지나갈 수 있으니, 두 명령을 빠르게 이어서 확인한다.

```bash
kubectl delete myapp sample-myapp &
kubectl get myapp sample-myapp -o jsonpath='ts={.metadata.deletionTimestamp} fin={.metadata.finalizers}{"\n"}'
```

기대 흐름.

```text
ts=2026-06-01T... fin=["myapp.apps.jangwoo.dev/finalizer"]   ← terminating 중
(잠시 후)
Error from server (NotFound): myapps.apps.jangwoo.dev "sample-myapp" not found  ← 실제 삭제 완료
```

`make run` 로그.

```text
INFO cleanup external resources {"name":"sample-myapp","bucket":"demo-bucket-sample-myapp"}
INFO finalizer removed; object will be deleted
```

```text
삭제 요청 → deletionTimestamp 찍힘 → cleanup 실행 → finalizer 제거 → 실제 삭제
```

이론(2단계 삭제)이 실제로 관찰되는 지점이다.

---

### 3-2. cleanup이 느릴 때를 흉내 내기 (선택)

terminating 상태를 눈으로 더 오래 보고 싶으면 cleanup에 잠깐 지연과 재시도를 넣어 본다.

```go
func (r *MyAppReconciler) cleanupExternalResources(
    ctx context.Context, myApp *appsv1alpha1.MyApp,
) error {
    // 관찰용: 5초 동안은 "아직 정리 중"이라고 실패시킨다
    if time.Since(myApp.DeletionTimestamp.Time) < 5*time.Second {
        return fmt.Errorf("external cleanup not finished yet")
    }
    log.FromContext(ctx).Info("cleanup done", "name", myApp.Name)
    return nil
}
```

이렇게 두면 삭제 후 약 5초 동안 객체가 terminating으로 남고, 그 사이 `make run` 로그에 재시도가 보인다.

```bash
kubectl delete myapp sample-myapp
kubectl get myapp sample-myapp -w
# terminating 상태가 몇 초 유지되다가 사라짐
```

관찰 후에는 지연 코드를 원복한다.

---

## 4. GC 정책 3종 비교

OwnerReference로 묶인 Deployment가 정책에 따라 어떻게 정리되는지 본다. 매번 CR을 다시 만들고 정책을 바꿔 삭제한다.

### 4-1. Background (기본)

```bash
kubectl apply -f config/samples/apps_v1alpha1_myapp.yaml
kubectl delete myapp sample-myapp --cascade=background
kubectl get deploy,rs,pod -l app.kubernetes.io/instance=sample-myapp
```

```text
부모를 즉시 삭제하고, 자식(Deployment/RS/Pod)은 백그라운드에서 곧 사라진다.
조회 시 잠깐 자식이 남아 보일 수 있다 (비동기).
```

---

### 4-2. Foreground

```bash
kubectl apply -f config/samples/apps_v1alpha1_myapp.yaml
kubectl delete myapp sample-myapp --cascade=foreground
kubectl get myapp sample-myapp -o jsonpath='{.metadata.deletionTimestamp}{"\n"}'
kubectl get deploy,rs,pod -l app.kubernetes.io/instance=sample-myapp
```

```text
부모에 deletionTimestamp가 찍힌 채 "살아 있고",
blockOwnerDeletion=true 자식(Deployment)이 먼저 지워진다.
자식이 다 사라진 뒤에야 부모(MyApp)가 사라진다.
→ "자식 → 부모" 순서가 보장된다.
```

> 우리 프로젝트에는 finalizer도 있으므로, foreground GC와 finalizer가 함께 동작한다. finalizer cleanup이 끝나야 부모 삭제가 마무리된다.

---

### 4-3. Orphan

```bash
kubectl apply -f config/samples/apps_v1alpha1_myapp.yaml
kubectl delete myapp sample-myapp --cascade=orphan
kubectl get deploy sample-myapp -o jsonpath='{.metadata.ownerReferences}{"\n"}'
```

```text
부모(MyApp)만 사라지고, Deployment는 살아남는다.
살아남은 Deployment의 ownerReferences에서 MyApp이 빠져 있다 (고아).
```

확인 후 남은 자식을 직접 정리한다.

```bash
kubectl delete deploy sample-myapp
```

세 정책을 표로 정리하면 다음과 같다.

| 정책 | 부모 | 자식 | 순서 |
| --- | --- | --- | --- |
| `background`(기본) | 즉시 삭제 | 비동기로 뒤따라 삭제 | 보장 안 됨 |
| `foreground` | 자식 정리 후 삭제 | 부모보다 먼저 삭제 | 자식 → 부모 |
| `orphan` | 삭제 | 남김(ownerRef 제거) | — |

> 이론: [week5-finalizer-gc.md](./week5-finalizer-gc.md) §2를 참고한다.

---

## 5. stuck terminating 재현과 복구

finalizer가 제거되지 못하면 객체가 영원히 안 지워진다는 것을 직접 본다(이론 §3-4).

### 5-1. 일부러 cleanup을 실패시키기

```go
func (r *MyAppReconciler) cleanupExternalResources(
    ctx context.Context, myApp *appsv1alpha1.MyApp,
) error {
    return fmt.Errorf("forced cleanup failure")   // 관찰용
}
```

```bash
make run
kubectl apply -f config/samples/apps_v1alpha1_myapp.yaml
kubectl delete myapp sample-myapp
kubectl get myapp sample-myapp
```

기대 동작.

```text
NAME           ...   (Terminating 상태로 사라지지 않음)
make run 로그: ERROR cleanup failed ... (지수 백오프로 계속 재시도)
```

```bash
kubectl get myapp sample-myapp \
  -o jsonpath='ts={.metadata.deletionTimestamp} fin={.metadata.finalizers}{"\n"}'
# ts=2026-... fin=["myapp.apps.jangwoo.dev/finalizer"]  ← 계속 남아 있음
```

---

### 5-2. 복구 방법

올바른 복구는 “cleanup이 성공하도록 고치는 것”이다. 강제 코드를 원복하면, 재시도 중이던 Reconcile이 cleanup에 성공하고 finalizer를 제거해 객체가 자연스럽게 사라진다.

```text
강제 실패 코드 원복 → make run 재시작
  → 다음 재시도에서 cleanup 성공 → RemoveFinalizer → 객체 삭제
```

최후의 수단(외부 자원 누수 위험)으로 finalizer를 강제로 비우는 방법도 있지만, 학습 목적이 아니면 쓰지 않는다.

```bash
# 외부 자원이 정리되지 않은 채 객체만 사라짐 — 정말 필요할 때만
kubectl patch myapp sample-myapp \
  --type=merge -p '{"metadata":{"finalizers":[]}}'
```

관찰이 끝나면 강제 실패 코드를 반드시 원복한다.

---

## 6. 정리

이번 실습에서 확인한 것.

```text
Status 고도화
  - Status().Update → Status().Patch(MergeFrom(base)) 전환
  - base.Status == 새 status 면 호출 생략 → 플래핑 제거
  - Ready condition을 computeReady 순수 함수로 분리 (멱등)
  - Conflict 재시도 경로 유지

Finalizer
  - AddFinalizer로 삭제 전에 미리 부착
  - 삭제 경로: cleanup(멱등) → 성공 시 RemoveFinalizer → 실제 삭제
  - deletionTimestamp가 찍힌 채 terminating으로 대기하는 모습 관찰

GC 정책
  - background(기본): 부모 즉시, 자식 비동기
  - foreground: 자식 먼저, 부모 나중 (순서 보장)
  - orphan: 자식 남김 (ownerRef 제거)

장애/복구
  - cleanup 실패 → stuck terminating 재현
  - finalizers / deletionTimestamp로 원인 확인
  - 정상 복구는 cleanup을 고치는 것 (강제 비우기는 최후의 수단)
```

Week4에서 잡은 4단계 흐름(`fetch → compare → act → status`)에, Week5에서 **삭제 경로 분기**와 **안전한 status 쓰기**가 더해졌다. 이제 Reconcile은 생성·갱신·삭제 전 생애주기를 멱등하게 다룬다.

```text
Reconcile()
  fetch
   ├─ DeletionTimestamp 있음 → cleanup → RemoveFinalizer        (week5-finalizer-gc)
   └─ DeletionTimestamp 없음
        ├─ AddFinalizer (없으면)                                (week5-finalizer-gc)
        ├─ ensureDeployment (CreateOrUpdate + OwnerReference)    (week4)
        └─ updateStatus (Patch + 플래핑 방지 + Conflict 재시도)  (week5-status-strategy)
```

---

## 참고 공식 문서

- [Kubernetes Docs — Using Finalizers](https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers/)
- [Kubernetes Docs — Garbage Collection (Owners/dependents, Cascading)](https://kubernetes.io/docs/concepts/architecture/garbage-collection/)
- [Kubernetes Docs — Use cascading deletion](https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/)
- [CustomResourceDefinitions — Status subresource](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#status-subresource)
- [Kubebuilder Book — Using Finalizers](https://book.kubebuilder.io/reference/using-finalizers.html)
- [controller-runtime — `client.MergeFrom`](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client#MergeFrom)
- [controller-runtime — `controllerutil` (Finalizer helpers)](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/controller/controllerutil)
- [apimachinery — `meta.SetStatusCondition`](https://pkg.go.dev/k8s.io/apimachinery/pkg/api/meta#SetStatusCondition)
