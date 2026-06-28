# Week 5 - Finalizer 패턴과 가비지 컬렉션(GC)

> Status 업데이트 전략 부분은 [week5-status-strategy.md](./week5-status-strategy.md)에서 먼저 다룬다.
> 실습 진행 기록은 [week5-practice.md](./week5-practice.md)를 참고한다.

---

## 이 문서의 흐름

Week4에서 OwnerReference로 자식 리소스를 GC에 연동했고(§4), Finalizer의 표준 흐름도 한 번 봤다(§4-3).
이 문서는 그 두 가지를 **삭제(deletion)라는 한 주제로 묶어서** 다시, 더 깊게 본다.

“만드는 것”은 Reconcile에서 자연스럽게 다뤘지만, “지우는 것”은 생각보다 규칙이 많다.

```text
Week4에서 본 것
  - OwnerReference로 cascading delete가 일어난다
  - Finalizer 추가/제거 코드 골격

Week5에서 더 보는 것 (이 문서)
  ├─ 삭제가 실제로 어떻게 일어나는가 (deletionTimestamp)
  ├─ 가비지 컬렉션: Foreground / Background / Orphan
  ├─ OwnerReference 규칙 (네임스페이스, controller, blockOwnerDeletion)
  ├─ Finalizer 패턴: 외부 자원을 안전하게 정리하기
  └─ 자주 하는 실수와 멱등성
```

이 문서는 다음 순서로 읽으면 된다.

1. “삭제 요청”이 들어왔을 때 내부에서 무슨 일이 벌어지는지 본다.
2. 가비지 컬렉터의 cascading 삭제 정책 세 가지를 구분한다.
3. OwnerReference의 규칙과 제약을 정리한다.
4. Finalizer로 외부 자원을 정리하는 표준 패턴을 본다.
5. 두 메커니즘을 언제 쓰는지 정리한다.

---

## 배경: 삭제는 “즉시”가 아니다

`kubectl delete`를 하면 객체가 바로 사라진다고 생각하기 쉽다. 하지만 Kubernetes의 삭제는 **2단계**다.

```text
1단계: 삭제 "요청"
  - API Server가 metadata.deletionTimestamp 를 채운다
  - 객체는 아직 etcd에 남아 있다 (finalizers가 남아 있는 한)

2단계: 실제 "제거"
  - finalizers 목록이 비고
  - dependent(자식) 정리 조건이 충족되면
  - 그제서야 API Server가 객체를 etcd에서 지운다
```

> 공식 문서 (Kubernetes Docs, "Using Finalizers"): "When you tell Kubernetes to delete an object that has finalizers specified for it, the Kubernetes API marks the object for deletion by populating `.metadata.deletionTimestamp`, and returns a `202` status code (HTTP 'Accepted'). The target object remains in a terminating state while the control plane ... takes the actions defined by the finalizers. After these actions are complete, the controller removes the relevant finalizers from the target object. When the `metadata.finalizers` field is empty, Kubernetes considers the deletion complete and deletes the object."

이 “2단계 삭제” 덕분에 Operator는 객체가 사라지기 전에 끼어들어 정리 작업을 할 수 있다. 그 끼어드는 고리가 **Finalizer**이고, 자식을 자동으로 따라 지우는 메커니즘이 **Garbage Collection**이다.

---

## 1. 삭제의 내부 동작: deletionTimestamp

### 1-1. deletionTimestamp가 하는 일

`metadata.deletionTimestamp`는 “이 객체는 삭제 요청을 받았다”는 표시다.

```text
deletionTimestamp 가 비어 있음(zero)
  → 정상 객체. 평소처럼 Reconcile (생성/갱신)

deletionTimestamp 가 채워져 있음
  → 삭제 중. 이제 할 일은 "정리"뿐
    (새 자식 리소스를 만들면 안 된다)
```

그래서 Reconcile은 fetch 직후 항상 이 값을 검사해서 분기한다(Week4 §4-3).

```go
if myApp.DeletionTimestamp.IsZero() {
    // 정상 경로: finalizer 보장 + 평소 Reconcile
} else {
    // 삭제 경로: 외부 자원 정리 후 finalizer 제거
}
```

---

### 1-2. finalizer가 없으면?

만약 객체에 finalizer가 하나도 없으면, 1단계와 2단계가 사실상 동시에 일어난다. `deletionTimestamp`를 볼 새도 없이 객체가 사라진다.

```text
finalizers = []         → delete 요청 즉시 제거 (Reconcile이 못 끼어듦)
finalizers = ["x/y"]    → deletionTimestamp만 찍히고 대기
                          → finalizer 제거될 때까지 객체가 남아 있음
```

즉 **“삭제 전에 뭔가 해야 한다”면 반드시 finalizer를 먼저 달아 둬야 한다.** 삭제 요청이 들어온 뒤에 다는 건 늦다.

---

## 2. 가비지 컬렉션: cascading 삭제

### 2-1. OwnerReference 복습

Week4 §4에서 봤듯이, 자식 리소스에 부모를 가리키는 OwnerReference를 달면 가비지 컬렉터가 “부모가 사라지면 이 자식도 지워도 된다”고 판단한다.

> 공식 문서 (Kubernetes Docs, "Owners and dependents"): "An owner reference tells Kubernetes which object is the 'owner' of another object. ... The garbage collector uses owner references to determine which dependent objects can be deleted."

```text
MyApp (owner)
  │  ownerReference (controller=true)
  ▼
Deployment (dependent)
  │  (Deployment가 만드는)
  ▼
ReplicaSet → Pod
```

부모(MyApp)를 지우면 이 사슬을 따라 자식들이 정리된다. 이것을 **cascading deletion**이라고 한다.

---

### 2-2. 세 가지 삭제 정책

cascading 삭제에는 세 가지 정책(propagationPolicy)이 있다.

> 공식 문서 (Kubernetes Docs, "Cascading deletion"): "Kubernetes checks for and deletes objects that no longer have owner references... There are two types of cascading deletion, namely foreground and background. There is also the option to orphan dependents."

```text
Foreground (전경)
  - 부모에 deletionTimestamp가 찍히고 "deletion in progress" 상태로 들어감
  - blockOwnerDeletion=true 인 자식들을 "먼저" 지운다
  - 자식이 다 사라진 뒤에야 부모가 사라진다
  - "자식부터, 그다음 부모" 순서를 보장하고 싶을 때

Background (후경, 기본값)
  - 부모를 "즉시" 지운다
  - 자식은 가비지 컬렉터가 백그라운드에서 뒤따라 지운다
  - 가장 흔한 기본 동작

Orphan (고아)
  - 부모만 지우고 자식은 남긴다
  - 자식의 ownerReference에서 그 부모를 떼어낸다 (고아로 만듦)
```

그림으로 비교하면 다음과 같다.

```text
[Foreground]
  delete owner 요청
     │
     ▼
  owner: deletionTimestamp 찍힘 (아직 살아있음)
     │   blockOwnerDeletion=true 자식 먼저 삭제
     ▼
  자식들 삭제 완료
     │
     ▼
  owner 삭제 완료          ← 자식 → 부모 순서 보장

[Background] (기본)
  delete owner 요청
     │
     ▼
  owner 즉시 삭제
     │   가비지 컬렉터가 뒤이어
     ▼
  자식들 비동기 삭제        ← 순서 보장 X, 빠름

[Orphan]
  delete owner 요청
     │
     ▼
  owner 삭제, 자식은 ownerReference만 제거되고 살아남음
```

kubectl로는 다음과 같이 정책을 고를 수 있다.

```bash
kubectl delete myapp sample-myapp --cascade=background   # 기본
kubectl delete myapp sample-myapp --cascade=foreground
kubectl delete myapp sample-myapp --cascade=orphan
```

---

### 2-3. blockOwnerDeletion과 Foreground

`blockOwnerDeletion: true`는 Foreground 삭제에서만 의미가 있다. “이 자식이 아직 살아 있는 동안에는 부모를 완전히 지우지 마라”는 뜻이다.

```text
ownerReference:
  controller: true
  blockOwnerDeletion: true
      │
      ▼
Foreground 삭제 시:
  이 자식이 지워지기 전까지 owner의 최종 삭제가 "차단(block)"된다
```

Week4에서 본 `controllerutil.SetControllerReference`는 이 값을 자동으로 `true`로 채워 준다(Week4 §4-2). 그래서 별도 설정 없이도 Foreground 삭제가 올바르게 동작한다.

---

### 2-4. OwnerReference의 규칙

OwnerReference에는 몇 가지 제약이 있다. 잘못 쓰면 GC가 동작하지 않거나 객체가 곧장 지워진다.

```text
1. 네임스페이스 규칙
   - cross-namespace 참조 불가
   - namespaced 자식의 owner는 "같은 네임스페이스" 객체이거나
     cluster-scoped 객체여야 한다
   - cluster-scoped 객체가 namespaced 객체를 owner로 가질 수는 없다

2. controller 플래그
   - 한 자식에 여러 ownerReference가 있을 수 있지만
   - controller=true 인 것은 "단 하나"만 허용된다 (관리 주체는 하나)

3. UID로 식별
   - ownerReference는 이름이 아니라 UID로 부모를 식별한다
   - 같은 이름의 객체가 지워지고 다시 만들어지면 UID가 달라
     예전 ownerReference는 "존재하지 않는 부모"를 가리키게 되어
     해당 자식이 GC 대상이 된다
```

> 공식 문서 (Kubernetes Docs, "Owners and dependents"): "Cross-namespace owner references are disallowed by design. Namespaced dependents can specify cluster-scoped or namespaced owners. ... If you specify an owner reference to an owner in a different namespace, the owner reference is treated as having a missing owner, and the dependent is subject to deletion."

핵심은 Week4와 같다.

```text
클러스터 안에서 끝나는 자식 (Deployment, Service, ConfigMap ...)
  → OwnerReference만 달면 GC가 알아서 정리한다. Finalizer 불필요.
```

---

## 3. Finalizer: 외부 자원을 안전하게 정리

### 3-1. GC만으로 부족한 경우

OwnerReference 기반 GC는 **Kubernetes 객체끼리**만 동작한다. 클러스터 바깥의 자원은 GC가 알지 못한다.

```text
GC가 정리할 수 있는 것
  - Deployment, Service, ConfigMap, PVC ... (k8s 객체)

GC가 모르는 것 (직접 정리해야 함)
  - AWS S3 버킷 / IAM 역할
  - 외부 DB의 스키마/계정
  - Vault에 등록한 시크릿
  - 외부 모니터링 시스템에 등록한 대상
  - 클러스터 밖 DNS 레코드 등
```

CR이 지워질 때 이런 외부 자원도 같이 정리하려면, “객체가 사라지기 직전에 끼어들 고리”가 필요하다. 그게 Finalizer다.

> 공식 문서 (Kubebuilder Book, "Using Finalizers"): "Finalizers allow controllers to implement asynchronous pre-delete hooks. ... When deleting an object, you can add a finalizer to perform any pre-delete cleanup, then remove the finalizer to allow the object to be deleted."

---

### 3-2. Finalizer 표준 흐름

Finalizer는 `metadata.finalizers`라는 **문자열 목록**이다. 컨트롤러는 이 목록을 추가/제거하면서 삭제 시점을 제어한다.

```text
finalizer 이름 규칙: <domain>/<name>
  예: "myapp.apps.jangwoo.dev/finalizer"
```

전체 생애주기를 그림으로 보면 다음과 같다.

```text
                  CR 생성
                    │
                    ▼
       ┌─────────────────────────────┐
       │ 정상 Reconcile               │
       │  - finalizer 없으면 추가     │  ← 삭제 전에 미리 달아 둔다
       │  - 평소처럼 자식 리소스 관리 │
       └──────────────┬──────────────┘
                       │  사용자가 kubectl delete
                       ▼
       ┌─────────────────────────────┐
       │ deletionTimestamp 찍힘       │  ← 객체는 아직 안 사라짐
       │ (finalizer가 남아 있으니까)  │
       └──────────────┬──────────────┘
                       │  다시 Reconcile (삭제 경로)
                       ▼
       ┌─────────────────────────────┐
       │ 외부 자원 정리 (멱등)        │
       │  - 성공 → finalizer 제거     │
       │  - 실패 → error 반환(재시도) │
       └──────────────┬──────────────┘
                       │  finalizers = [] 가 되면
                       ▼
       ┌─────────────────────────────┐
       │ API Server가 객체를 실제 삭제 │
       └─────────────────────────────┘
```

코드 골격(Week4 §4-3을 다시, 외부 자원 정리를 강조해서).

```go
const finalizerName = "myapp.apps.jangwoo.dev/finalizer"

func (r *MyAppReconciler) Reconcile(
    ctx context.Context, req ctrl.Request,
) (ctrl.Result, error) {
    var myApp appsv1alpha1.MyApp
    if err := r.Get(ctx, req.NamespacedName, &myApp); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // ── 정상 경로: 삭제 중이 아님 ──
    if myApp.DeletionTimestamp.IsZero() {
        if !controllerutil.ContainsFinalizer(&myApp, finalizerName) {
            controllerutil.AddFinalizer(&myApp, finalizerName)
            if err := r.Update(ctx, &myApp); err != nil {
                return ctrl.Result{}, err
            }
        }
        // ... 평소 Reconcile (자식 리소스 ensure, status 갱신) ...
        return ctrl.Result{}, nil
    }

    // ── 삭제 경로: deletionTimestamp가 찍힘 ──
    if controllerutil.ContainsFinalizer(&myApp, finalizerName) {
        // 1) 외부 자원 정리 (멱등이어야 함)
        if err := r.cleanupExternalResources(ctx, &myApp); err != nil {
            // 실패 → error 반환 → 지수 백오프로 자동 재시도
            return ctrl.Result{}, err
        }
        // 2) 정리 성공 후에만 finalizer 제거
        controllerutil.RemoveFinalizer(&myApp, finalizerName)
        if err := r.Update(ctx, &myApp); err != nil {
            return ctrl.Result{}, err
        }
    }
    // finalizer가 비면 API Server가 객체를 실제로 지운다
    return ctrl.Result{}, nil
}
```

> 주의: finalizer를 추가/제거하는 것은 `metadata` 변경이므로 `r.Status().Update`가 아니라 **`r.Update`**(spec 경로)로 쓴다. status 갱신과는 경로가 다르다(Week5 status 문서 §1 참고).

---

### 3-3. 멱등성: 정리 함수의 생명

`cleanupExternalResources`는 **여러 번 호출되어도 안전**해야 한다. Reconcile은 재시도로 같은 삭제 경로를 여러 번 탈 수 있기 때문이다.

```go
func (r *MyAppReconciler) cleanupExternalResources(
    ctx context.Context, myApp *appsv1alpha1.MyApp,
) error {
    // "이미 없으면 성공"으로 취급한다
    if err := externalAPI.DeleteBucket(ctx, myApp.Status.BucketName); err != nil {
        if isNotFound(err) {
            return nil   // 이미 지워짐 → 성공
        }
        return err       // 진짜 실패 → 재시도
    }
    return nil
}
```

멱등성 규칙을 정리하면 다음과 같다.

```text
1. "이미 없음"은 성공으로 취급한다 (NotFound → nil)
2. 정리가 끝났다는 확신이 설 때만 finalizer를 제거한다
3. 정리 도중 실패하면 error를 반환한다
   → controller-runtime이 지수 백오프로 다시 부른다 (Week4 §5-4)
   → 그동안 객체는 deletionTimestamp가 찍힌 채 남아 있다
```

---

### 3-4. finalizer가 객체를 “못 지우게” 만드는 함정

finalizer는 강력한 만큼 위험하다. **컨트롤러가 finalizer를 제거하지 못하면 객체가 영원히 안 지워진다.**

```text
흔한 멈춤(stuck terminating) 원인
  - 컨트롤러가 죽어 있어서 삭제 경로를 아무도 처리 못 함
  - cleanup이 계속 실패하는데 영영 성공 못 함 (잘못된 외부 권한 등)
  - finalizer 이름 오타로 ContainsFinalizer가 항상 false
```

이렇게 멈춘 객체는 `deletionTimestamp`는 찍혀 있는데 사라지지 않는다. 디버깅 시 확인 포인트:

```bash
kubectl get myapp sample-myapp -o jsonpath='{.metadata.finalizers}{"\n"}'
kubectl get myapp sample-myapp -o jsonpath='{.metadata.deletionTimestamp}{"\n"}'
```

> 최후의 수단으로 finalizer를 강제로 비워 객체를 지울 수 있지만, 이는 **외부 자원이 정리되지 않은 채 남는다**는 뜻이므로 학습/디버깅 목적이 아니면 피한다.

```bash
# 외부 자원이 영구히 누수될 수 있음 — 정말 필요한 경우만
kubectl patch myapp sample-myapp \
  --type=merge -p '{"metadata":{"finalizers":[]}}'
```

---

## 4. GC vs Finalizer: 언제 무엇을 쓰나

두 메커니즘은 경쟁 관계가 아니라 **역할이 다르다**. 보통 함께 쓴다.

```text
OwnerReference + GC
  - 대상: 클러스터 안의 자식 k8s 객체
  - 동작: 부모가 사라지면 자식도 자동 정리 (cascading)
  - 코드: SetControllerReference 한 줄 (Week4 §4-2)

Finalizer
  - 대상: 클러스터 밖 외부 자원, 또는 "삭제 전 꼭 해야 할 일"
  - 동작: 삭제를 잠시 막고, 정리 끝나면 풀어준다
  - 코드: Add/Contains/Remove Finalizer + cleanup (이 문서 §3)
```

판단 기준을 한 표로 정리하면 다음과 같다.

| 정리해야 할 대상 | 메커니즘 |
| --- | --- |
| Deployment / Service / ConfigMap / PVC 등 k8s 자식 | OwnerReference (GC) |
| 외부 클라우드 자원 (S3, IAM, DNS …) | Finalizer |
| 외부 시스템 등록 해제 (모니터링, Vault …) | Finalizer |
| 삭제 순서 보장이 필요한 k8s 자식 | OwnerReference + Foreground 정책 |
| 둘 다 (자식 + 외부 자원) | OwnerReference + Finalizer 함께 |

```text
결정 트리
  외부(클러스터 밖) 자원을 만들었나?
    ├─ 예  → Finalizer 필요 (cleanup 구현)
    └─ 아니오 → OwnerReference만으로 충분 (GC에 위임)
```

---

## 5. 한 번의 삭제, 전체 흐름

Status 문서와 짝을 맞춰, 삭제 경로 전체를 한 그림으로 묶으면 다음과 같다.

```text
   kubectl delete myapp sample-myapp
              │
              ▼
   ┌────────────────────────────────────────┐
   │ API Server                             │
   │  - finalizers 있음? ─ 예 ─┐            │
   │  - deletionTimestamp 기록 │            │
   └───────────────────────────┼────────────┘
                                ▼
   ┌────────────────────────────────────────┐
   │ Reconcile (삭제 경로)                   │
   │                                        │
   │  if !DeletionTimestamp.IsZero():        │
   │    1) cleanupExternalResources (멱등)   │
   │       - 실패 → return err (재시도)      │
   │    2) RemoveFinalizer + r.Update        │
   └───────────────────────────┬────────────┘
                                ▼  finalizers == []
   ┌────────────────────────────────────────┐
   │ API Server: 객체 실제 삭제             │
   └───────────────────────────┬────────────┘
                                ▼  (OwnerReference)
   ┌────────────────────────────────────────┐
   │ Garbage Collector                      │
   │  - Deployment / RS / Pod cascading 삭제 │
   │  - 정책: Foreground / Background / Orphan│
   └────────────────────────────────────────┘
```

핵심 한 줄로 요약하면 다음과 같다.

> 삭제는 “deletionTimestamp로 예약되고, Finalizer로 정리를 기다리며, finalizer가 비면 실제로 지워지고, OwnerReference를 따라 자식이 GC된다”.

---

## 6. 정리

| 주제 | 핵심 정리 |
| --- | --- |
| deletionTimestamp | 삭제는 2단계. 요청 시 타임스탬프만 찍히고, finalizer가 비어야 실제 삭제 |
| finalizer 부재 | finalizer가 없으면 삭제 요청 즉시 사라짐 → 끼어들 수 없음. 미리 달아 둔다 |
| GC 정책 | Foreground(자식→부모), Background(기본, 즉시+비동기), Orphan(자식 남김) |
| blockOwnerDeletion | Foreground에서 “자식이 지워지기 전 부모 최종 삭제 차단”. SetControllerReference가 채움 |
| OwnerReference 규칙 | cross-namespace 불가, controller=true는 하나, UID로 식별 |
| Finalizer 패턴 | deletionTimestamp 검사 → 외부 자원 정리 → 성공 후 finalizer 제거 |
| 멱등성 | cleanup은 여러 번 호출돼도 안전해야 함. NotFound는 성공으로 |
| stuck terminating | cleanup 실패/컨트롤러 다운 시 객체가 안 지워짐. finalizers/deletionTimestamp 확인 |
| GC vs Finalizer | k8s 자식 = OwnerReference, 외부 자원 = Finalizer. 보통 함께 |

이 문서의 결과물은 Week4의 Reconcile 본문에 “삭제 경로 분기”가 단단하게 들어가는 형태로 나타난다.

```text
internal/controller/myapp_controller.go
  Reconcile()
    ├─ if DeletionTimestamp.IsZero():
    │     ├─ AddFinalizer (없으면)
    │     └─ 평소 Reconcile (ensure + status)
    └─ else (삭제 중):
          ├─ cleanupExternalResources (멱등)
          └─ RemoveFinalizer
```

다음 문서(실습)에서 이 흐름을 실제 `MyApp` 프로젝트에 구현하고 동작을 관찰한다.

---

## 참고 공식 문서

- [Kubernetes Docs — Using Finalizers](https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers/)
- [Kubernetes Docs — Owners and dependents](https://kubernetes.io/docs/concepts/architecture/garbage-collection/#owners-dependents)
- [Kubernetes Docs — Cascading deletion (Foreground/Background)](https://kubernetes.io/docs/concepts/architecture/garbage-collection/#cascading-deletion)
- [Kubernetes Docs — Use foreground cascading deletion](https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/)
- [Kubebuilder Book — Using Finalizers](https://book.kubebuilder.io/reference/using-finalizers.html)
- [controller-runtime — `controllerutil` (AddFinalizer / RemoveFinalizer / ContainsFinalizer)](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/controller/controllerutil)
- [controller-runtime — `controllerutil.SetControllerReference`](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/controller/controllerutil#SetControllerReference)
