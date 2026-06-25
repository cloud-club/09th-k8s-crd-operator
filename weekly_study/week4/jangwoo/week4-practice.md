# Week 4 - 실습

> 이론은 [week4-crd-design.md](./week4-crd-design.md)와 [week4-reconcile.md](./week4-reconcile.md)를 먼저 읽고 진행한다.
> Week3 실습에서 만든 `MyApp` CRD 프로젝트(`~/Desktop/Study/k8s-study/`)를 그대로 이어 쓴다.

---

## 이 실습의 흐름

Week3 실습은 “Manager와 MyApp Controller가 시작되는 것까지”였다. status는 비어 있었고 하위 리소스도 생성하지 않았다.

Week4에서는 그 위에 Reconcile을 채워 넣는다.

```text
Week3 결과
  - MyApp CR 생성 가능
  - Manager 시작 / Controller 등록
  - Reconcile은 비어 있음 (status 갱신 X, 하위 리소스 생성 X)

Week4 목표
  - MyApp.spec → Deployment 생성/갱신
  - OwnerReference + GC 동작 확인
  - status.readyReplicas / conditions / observedGeneration 갱신
  - 에러 시나리오에서 conditions 메시지 확인
```

진행 순서.

```text
1. 프로젝트 확인 (week3 결과 이어받기)
2. types.go에 status conditions / observedGeneration 추가
3. Reconcile에 Deployment Create/Update 구현
4. OwnerReference / GC 동작 확인
5. status 갱신 구현 (Ready / Progressing / ReconcileError)
6. 에러 시나리오 → conditions 메시지 관찰
7. 정리
```

---

## 사전 준비

Week3에서 사용한 프로젝트를 그대로 사용한다.

```text
모듈: github.com/jangwoojung/test-operator
GVK : apps.jangwoo.dev/v1alpha1, Kind=MyApp
경로: ~/Desktop/Study/k8s-study/
```

준비 확인.

```bash
cd ~/Desktop/Study/k8s-study
ls api/v1alpha1/myapp_types.go internal/controller/myapp_controller.go
kubectl get crd myapps.apps.jangwoo.dev
```

`make run`이 끝까지 동작해야 다음 단계를 진행할 수 있다(Week3 §6 참고).

---

## 1. CRD 강화: status에 conditions / observedGeneration 추가

### 1-1. types.go 수정

`api/v1alpha1/myapp_types.go`에서 `MyAppStatus`를 다음과 같이 확장한다.

```go
import (
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MyAppStatus defines the observed state of MyApp.
type MyAppStatus struct {
    // 마지막으로 Reconcile이 반영한 spec generation
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`

    // Ready 상태인 Pod 수
    ReadyReplicas int32 `json:"readyReplicas,omitempty"`

    // 다축 상태 표현 (Ready / Progressing / ReconcileError ...)
    // +optional
    // +patchMergeKey=type
    // +patchStrategy=merge
    // +listType=map
    // +listMapKey=type
    Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}
```

`+listType=map` / `+listMapKey=type` 마커는 동일 `Type` Condition이 두 개 생기지 않게 해 준다(공식 권장 패턴).

printcolumn에 `Ready` Condition을 노출해서 `kubectl get myapp` 한 줄로 상태를 볼 수 있게 한다.

```go
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=`.status.conditions[?(@.type=="Ready")].reason`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type MyApp struct { /* ... */ }
```

---

### 1-2. CRD 재생성 / 재설치

```bash
make generate
make manifests
make install
```

확인.

```bash
kubectl explain myapp.status
kubectl explain myapp.status.conditions
```

기대 출력에 `observedGeneration`, `conditions[]`, `conditions.type/status/reason/message/lastTransitionTime/observedGeneration`이 보여야 한다.

---

## 2. Reconcile: Deployment Create/Update 구현

### 2-1. import / RBAC marker 추가

`internal/controller/myapp_controller.go` 상단을 다음과 같이 정리한다.

```go
package controller

import (
    "context"

    appsv1 "k8s.io/api/apps/v1"
    corev1 "k8s.io/api/core/v1"
    apierrors "k8s.io/apimachinery/pkg/api/errors"
    "k8s.io/apimachinery/pkg/api/meta"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/utils/ptr"

    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
    "sigs.k8s.io/controller-runtime/pkg/log"

    appsv1alpha1 "github.com/jangwoojung/test-operator/api/v1alpha1"
)
```

권한 marker는 reconciler 메서드 바로 위에 둔다. Deployment 관리에 필요한 권한과 자기 CR `status` 권한을 따로 적는다.

```go
// +kubebuilder:rbac:groups=apps.jangwoo.dev,resources=myapps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.jangwoo.dev,resources=myapps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.jangwoo.dev,resources=myapps/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
```

> 공식 문서 (Kubebuilder Book, "Generating CRDs and RBAC"): "RBAC manifests are generated from `+kubebuilder:rbac` markers near the reconciler."

---

### 2-2. buildDeployment: desired를 함수로 분리

```go
func buildDeployment(myApp *appsv1alpha1.MyApp) *appsv1.Deployment {
    labels := map[string]string{
        "app.kubernetes.io/name":       "myapp",
        "app.kubernetes.io/instance":   myApp.Name,
        "app.kubernetes.io/managed-by": "myapp-operator",
    }
    return &appsv1.Deployment{
        ObjectMeta: metav1.ObjectMeta{
            Name:      myApp.Name,
            Namespace: myApp.Namespace,
            Labels:    labels,
        },
        Spec: appsv1.DeploymentSpec{
            Replicas: ptr.To(myApp.Spec.Replicas),
            Selector: &metav1.LabelSelector{MatchLabels: labels},
            Template: corev1.PodTemplateSpec{
                ObjectMeta: metav1.ObjectMeta{Labels: labels},
                Spec: corev1.PodSpec{
                    Containers: []corev1.Container{{
                        Name:  "app",
                        Image: myApp.Spec.Image,
                        Ports: []corev1.ContainerPort{{ContainerPort: 80}},
                    }},
                },
            },
        },
    }
}
```

---

### 2-3. ensureDeployment: CreateOrUpdate + SetControllerReference

```go
func (r *MyAppReconciler) ensureDeployment(
    ctx context.Context, myApp *appsv1alpha1.MyApp,
) (*appsv1.Deployment, error) {
    dep := &appsv1.Deployment{
        ObjectMeta: metav1.ObjectMeta{
            Name:      myApp.Name,
            Namespace: myApp.Namespace,
        },
    }

    op, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
        // 1) OwnerReference 보장 (GC + Owns() 자동 enqueue)
        if err := controllerutil.SetControllerReference(myApp, dep, r.Scheme); err != nil {
            return err
        }
        // 2) 내가 책임지는 필드만 덮어쓰기
        desired := buildDeployment(myApp)
        dep.Labels = desired.Labels
        dep.Spec.Replicas = desired.Spec.Replicas
        dep.Spec.Selector = desired.Spec.Selector
        dep.Spec.Template = desired.Spec.Template
        return nil
    })
    if err != nil {
        return nil, err
    }

    log.FromContext(ctx).Info("ensureDeployment", "op", op)
    return dep, nil
}
```

`CreateOrUpdate`는 mutate 전/후를 비교해 변경이 없으면 API 호출을 건너뛴다. 따라서 무한 업데이트는 발생하지 않는다.

---

### 2-4. updateStatus: observedGeneration + Ready Condition

```go
func (r *MyAppReconciler) updateStatus(
    ctx context.Context, myApp *appsv1alpha1.MyApp,
) error {
    var dep appsv1.Deployment
    err := r.Get(ctx, client.ObjectKeyFromObject(myApp), &dep)
    if err != nil && !apierrors.IsNotFound(err) {
        return err
    }

    myApp.Status.ObservedGeneration = myApp.Generation
    myApp.Status.ReadyReplicas = dep.Status.ReadyReplicas

    cond := metav1.Condition{
        Type:               "Ready",
        ObservedGeneration: myApp.Generation,
    }
    switch {
    case apierrors.IsNotFound(err):
        cond.Status = metav1.ConditionFalse
        cond.Reason = "DeploymentMissing"
        cond.Message = "Deployment is not created yet."
    case dep.Status.ReadyReplicas >= myApp.Spec.Replicas:
        cond.Status = metav1.ConditionTrue
        cond.Reason = "AllReplicasReady"
        cond.Message = "All replicas are ready."
    default:
        cond.Status = metav1.ConditionFalse
        cond.Reason = "WaitingForReplicas"
        cond.Message = "Some replicas are not ready yet."
    }
    meta.SetStatusCondition(&myApp.Status.Conditions, cond)

    return r.Status().Update(ctx, myApp)
}
```

---

### 2-5. Reconcile 본문: fetch → ensure → status

```go
func (r *MyAppReconciler) Reconcile(
    ctx context.Context, req ctrl.Request,
) (ctrl.Result, error) {
    logger := log.FromContext(ctx)

    // 1) fetch
    var myApp appsv1alpha1.MyApp
    if err := r.Get(ctx, req.NamespacedName, &myApp); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // 2) act: Deployment 보장
    if _, err := r.ensureDeployment(ctx, &myApp); err != nil {
        logger.Error(err, "ensureDeployment failed")
        return r.requeueOnError(ctx, &myApp, err)
    }

    // 3) status 갱신
    if err := r.updateStatus(ctx, &myApp); err != nil {
        if apierrors.IsConflict(err) {
            return ctrl.Result{Requeue: true}, nil
        }
        return ctrl.Result{}, err
    }
    return ctrl.Result{}, nil
}

func (r *MyAppReconciler) requeueOnError(
    ctx context.Context, myApp *appsv1alpha1.MyApp, err error,
) (ctrl.Result, error) {
    meta.SetStatusCondition(&myApp.Status.Conditions, metav1.Condition{
        Type:               "Ready",
        Status:             metav1.ConditionFalse,
        Reason:             "ReconcileError",
        Message:            err.Error(),
        ObservedGeneration: myApp.Generation,
    })
    _ = r.Status().Update(ctx, myApp)
    return ctrl.Result{}, err
}
```

---

### 2-6. SetupWithManager에 Owns 등록

```go
func (r *MyAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&appsv1alpha1.MyApp{}).
        Owns(&appsv1.Deployment{}).
        Named("myapp").
        Complete(r)
}
```

`Owns()`가 있어야 Deployment가 누가 손대 바뀌어도 같은 MyApp으로 Reconcile이 자동으로 다시 들어온다.

---

### 2-7. 실행

```bash
make manifests
make generate
make install
make run
```

`make run` 로그에서 다음 두 줄이 보이면 정상이다.

```text
INFO Starting Controller {"controller":"myapp", ...}
INFO Starting workers    {"controller":"myapp", "worker count":1}
```

샘플 CR을 적용한다.

```bash
kubectl apply -f config/samples/apps_v1alpha1_myapp.yaml
```

확인.

```bash
kubectl get myapp
# NAME           REPLICAS   READY   STATUS   REASON              AGE
# sample-myapp   2          2       True     AllReplicasReady    20s

kubectl get deploy
# NAME           READY   UP-TO-DATE   AVAILABLE   AGE
# sample-myapp   2/2     2            2           20s

kubectl get pods -l app.kubernetes.io/instance=sample-myapp
```

`READY` 컬럼이 `2`로 채워지면 status 갱신까지 정상 동작한 것이다.

---

## 3. OwnerReference / GC 동작 확인

### 3-1. ownerReference 확인

```bash
kubectl get deploy sample-myapp \
  -o=jsonpath='{.metadata.ownerReferences}{"\n"}'
```

기대 출력 (정렬은 다를 수 있음).

```json
[{"apiVersion":"apps.jangwoo.dev/v1alpha1","blockOwnerDeletion":true,
  "controller":true,"kind":"MyApp","name":"sample-myapp","uid":"..."}]
```

`controller: true`, `blockOwnerDeletion: true`가 들어 있어야 한다. `controllerutil.SetControllerReference`가 채워 준 값이다.

---

### 3-2. cascading delete 관찰

MyApp만 삭제하면 Deployment / ReplicaSet / Pod까지 한번에 사라지는지 본다.

```bash
kubectl get deploy,rs,pod -l app.kubernetes.io/instance=sample-myapp
kubectl delete myapp sample-myapp
kubectl get deploy,rs,pod -l app.kubernetes.io/instance=sample-myapp
```

마지막 명령은 비어 있어야 한다.

> 공식 문서 (Kubernetes Docs, "Owners and dependents"): "The garbage collector uses owner references to determine which dependent objects can be deleted."

---

## 4. Update 동작 확인 (diff 기반 처리)

다시 CR을 적용하고, spec 일부를 바꿔서 Reconcile이 정확히 “바뀐 필드만” 갱신하는지 본다.

```bash
kubectl apply -f config/samples/apps_v1alpha1_myapp.yaml
```

---

### 4-1. replicas 변경

```bash
kubectl patch myapp sample-myapp --type=merge -p '{"spec":{"replicas":3}}'
kubectl get deploy sample-myapp -w
```

`make run` 로그에 다음 줄이 보여야 한다.

```text
INFO ensureDeployment {"op":"updated"}
```

Deployment의 `spec.replicas`가 `3`으로 따라가고, Pod 1개가 추가 생성된다.

---

### 4-2. image 변경 (롤링 업데이트)

```bash
kubectl patch myapp sample-myapp --type=merge -p '{"spec":{"image":"nginx:1.27"}}'
kubectl rollout status deploy/sample-myapp
```

`kubectl get deploy sample-myapp -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'`이 `nginx:1.27`로 바뀌어야 한다.

---

### 4-3. No-op 확인

같은 spec으로 다시 apply했을 때 `op: unchanged` 로그가 찍히는지 본다.

```bash
kubectl apply -f config/samples/apps_v1alpha1_myapp.yaml
```

`make run` 로그.

```text
INFO ensureDeployment {"op":"unchanged"}
```

→ `CreateOrUpdate`가 mutate 전/후의 차이를 보고 API 호출을 건너뛴 결과다.

---

### 4-4. 사용자가 직접 Deployment를 손댔을 때

`Owns()` 덕분에 Operator가 다시 원래대로 되돌리는지 본다.

```bash
kubectl scale deploy sample-myapp --replicas=10
kubectl get deploy sample-myapp -w
```

`make run` 로그에 `ensureDeployment {"op":"updated"}`가 다시 찍히고, replicas가 `myapp.spec.replicas`로 되돌아간다. “MyApp이 진실의 원천”이라는 설계 결정이 실제로 강제되는 모습이다.

---

## 5. status 갱신 자세히 보기

### 5-1. Conditions / observedGeneration 관찰

```bash
kubectl get myapp sample-myapp -o yaml
```

기대 출력 (요약).

```yaml
spec:
  replicas: 3
  image: nginx:1.27
status:
  observedGeneration: 5
  readyReplicas: 3
  conditions:
  - type: Ready
    status: "True"
    reason: AllReplicasReady
    message: All replicas are ready.
    observedGeneration: 5
    lastTransitionTime: "2026-05-28T..."
```

확인 포인트.


| 필드                                                   | 의미                         |
| ---------------------------------------------------- | -------------------------- |
| `metadata.generation` == `status.observedGeneration` | 최신 spec까지 Reconcile이 따라잡았다 |
| `status.readyReplicas` == `spec.replicas`            | 모든 Pod가 Ready              |
| `Ready` Condition: True / `AllReplicasReady`         | 사용자에게 보여줄 결론               |


---

### 5-2. spec 변경 직후 한 박자 차이 보기

`replicas`를 늘리고 즉시 조회하면 `observedGeneration < generation`이 잠깐 보일 수 있다.

```bash
kubectl patch myapp sample-myapp --type=merge -p '{"spec":{"replicas":4}}' \
  && kubectl get myapp sample-myapp \
       -o jsonpath='gen={.metadata.generation} observedGen={.status.observedGeneration}{"\n"}'
```

```text
gen=6 observedGen=5      ← 한순간 차이
gen=6 observedGen=6      ← Reconcile 완료 후
```

이 두 값이 같아지면 “최신 spec까지 반영됨”, 다르면 “아직 처리 중”이다.

---

## 6. 에러 시나리오: 잘못된 image

### 6-1. 일부러 깨뜨리기

```bash
kubectl patch myapp sample-myapp --type=merge -p \
  '{"spec":{"image":"no-such-registry.example/no-such:tag"}}'
```

Deployment는 갱신되지만 Pod는 `ImagePullBackOff` 상태가 된다. MyApp의 `readyReplicas`는 다시 떨어진다.

확인.

```bash
kubectl get pods -l app.kubernetes.io/instance=sample-myapp
# NAME                              READY   STATUS             RESTARTS   AGE
# sample-myapp-7b9...               0/1     ImagePullBackOff   0          30s
```

```bash
kubectl get myapp sample-myapp -o yaml | grep -A 4 conditions:
```

기대 출력.

```yaml
conditions:
- type: Ready
  status: "False"
  reason: WaitingForReplicas
  message: Some replicas are not ready yet.
```

→ Reconcile은 실패하지 않았지만 “관찰된 사실”이 status에 그대로 드러난다.

---

### 6-2. 회복

```bash
kubectl patch myapp sample-myapp --type=merge -p '{"spec":{"image":"nginx:1.27"}}'
kubectl get myapp sample-myapp -w
```

Pod가 Ready로 회복되면 condition도 자동으로 `True / AllReplicasReady`로 돌아간다. `lastTransitionTime`이 갱신된 시점이 회복 시각이다.

---

### 6-3. (선택) 에러 반환 경로 강제로 보기

ensureDeployment 내부에서 강제로 에러를 반환해 보면 Workqueue RateLimiter가 동작하는지 볼 수 있다.

```go
// 일시적으로 추가해서 관찰만 하고 원복
return errors.New("forced transient error")
```

`make run` 로그에 재시도 간격이 점점 늘어나는 것이 보인다.

```text
ERROR ensureDeployment failed ...
ERROR ensureDeployment failed ...    (수 ms 뒤)
ERROR ensureDeployment failed ...    (수십 ms 뒤)
ERROR ensureDeployment failed ...    (수백 ms 뒤)
```

> 공식 문서 (controller-runtime godoc, `workqueue.DefaultControllerRateLimiter`): "Combines an exponential per-item backoff with an overall token-bucket limiter."

확인 후에는 강제 에러를 꼭 원복한다.

---

## 7. 정리

이번 실습에서 확인한 것.

```text
CRD 강화
  - status.conditions / observedGeneration 추가
  - printcolumn에 Ready/Reason 노출

Reconcile 구현
  - fetch → ensureDeployment (CreateOrUpdate + SetControllerReference) → updateStatus
  - buildDeployment 함수로 desired 분리
  - CreateOrUpdate가 op=created/updated/unchanged 로깅

OwnerReference / GC
  - controllerutil.SetControllerReference가 controller=true / blockOwnerDeletion=true 채움
  - MyApp 삭제 → Deployment / ReplicaSet / Pod 까지 cascading delete

diff 기반 처리
  - 같은 spec apply → op=unchanged (No-op)
  - 외부에서 Deployment scale → Owns()로 Reconcile 트리거 → 원래대로 복원

status 갱신
  - observedGeneration이 generation을 따라잡는 과정 관찰
  - Ready Condition (True/False)의 reason/message 흐름 관찰

에러 / Requeue
  - 잘못된 image: Reconcile은 성공, status에 WaitingForReplicas 기록
  - 강제 에러 반환: 지수 백오프로 재시도되는 것을 로그로 확인
```

Week5 이후로는 Finalizer와 외부 자원 정리, Webhook, 멀티 클러스터 등으로 이어진다. 이 실습에서 잡은 4단계 흐름(`fetch → compare → act → status`)이 어디서나 같은 모양으로 반복된다.

---

## 참고 공식 문서

- [Kubernetes Docs — Owners and dependents](https://kubernetes.io/docs/concepts/architecture/garbage-collection/#owners-dependents)
- [Kubernetes Docs — Using Finalizers](https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers/)
- [Kubebuilder Book — Implementing a Controller](https://book.kubebuilder.io/cronjob-tutorial/controller-implementation.html)
- [Kubebuilder Book — Markers (RBAC, validation, printcolumn)](https://book.kubebuilder.io/reference/markers.html)
- [controller-runtime — `controllerutil.CreateOrUpdate](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/controller/controllerutil#CreateOrUpdate)`
- [controller-runtime — `controllerutil.SetControllerReference](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/controller/controllerutil#SetControllerReference)`
- [apimachinery — `meta.SetStatusCondition](https://pkg.go.dev/k8s.io/apimachinery/pkg/api/meta#SetStatusCondition)`

