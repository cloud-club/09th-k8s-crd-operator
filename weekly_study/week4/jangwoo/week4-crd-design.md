# Week 4 - CRD 모델링과 상태 설계

> Reconcile 구현 부분은 [week4-reconcile.md](./week4-reconcile.md)에서 이어진다.
> 실습 진행 기록은 [week4-practice.md](./week4-practice.md)를 참고한다.

---

## 이 문서의 흐름

Week3에서는 Operator가 무엇이고, `controller-runtime`이 어떤 구조를 제공하는지를 정리했다.
Week4부터는 “그 구조 위에 무엇을 어떻게 올릴 것인가”를 다룬다. 이 문서는 그중 **설계** 부분만 묶어서 본다.

```
설계 단계 (이 문서)
  ├─ 리소스 모델링  : CRD의 spec/status 모양 결정
  ├─ 상태 머신       : status로 표현할 단계와 전이 정의
  └─ 리소스 관계     : 주 리소스 ↔ 하위 리소스의 의존성 처리

구현 단계 (week4-reconcile.md)
  ├─ Reconcile 구조  : fetch → compare → act → status
  ├─ CRUD            : Create / Update / Delete 멱등 처리
  ├─ diff            : Desired vs Current 비교
  ├─ Ownership       : OwnerReference로 GC 연동
  └─ 에러 처리        : ctrl.Result로 Requeue / Backoff
```

이 문서는 다음 순서로 읽으면 된다.

1. CRD를 설계할 때 던져야 하는 질문을 본다.
2. spec과 status를 어떻게 나누는지 정한다.
3. status로 상태 머신을 표현하는 방법을 정한다.
4. 주 리소스와 하위 리소스의 의존성을 정리한다.

---

## 배경: 설계가 먼저인 이유

Operator를 만들 때 가장 자주 하는 실수는 “코드부터 짜는 것”이다.

```text
좋지 않은 흐름
  1. types.go에 필드 마구 추가
  2. Reconcile에서 if/else로 분기 늘리기
  3. status는 일단 string phase 하나로
  4. 운영하다 보니 GC, 재시도, 의존성이 꼬임
```

운영 자동화는 결국 **반복되는 판단**을 코드로 옮기는 일이다. 그래서 코드를 짜기 전에 다음 질문에 먼저 답해야 한다.

```text
설계 단계에서 답해야 하는 질문
  - 사용자는 무엇을 선언하고 싶은가?         → CRD spec
  - 어떤 상태를 사용자에게 보여줘야 하는가?   → CRD status
  - 어떤 단계로 상태가 바뀌는가?             → 상태 머신
  - 무엇을 만들고, 무엇을 따라 살고 죽는가?   → Owner / Dependency
```

> 공식 문서 (Kubernetes API Conventions, "spec and status"): "By convention, the Kubernetes API makes a distinction between the specification of the desired state of an object (a nested object field called `spec`) and the status of the object at the current time (a nested object field called `status`)."

이 원칙이 이 문서의 모든 주제를 관통한다.

---

## 1. 리소스 모델링: CRD 설계

### 1-1. CRD 설계의 출발점

CRD 설계는 “사용자가 어떤 한 줄을 적게 할 것인가”에서 시작한다.

예를 들어 이미지 워커를 운영한다고 하면, 사용자가 직접 Deployment·Service·ConfigMap을 다 적게 만들 수도 있고, 다음처럼 단일 CR로 묶을 수도 있다.

```yaml
apiVersion: apps.example.com/v1alpha1
kind: ImageWorker
metadata:
  name: thumbnail-worker
spec:
  replicas: 3
  image: ghcr.io/example/worker:v1.4
  queue:
    name: thumbnail-queue
```

이 CR 하나가 만들어내야 할 하위 리소스는 다음과 같다.

```text
ImageWorker (CR)
  ├─ Deployment      (워커 Pod 실행)
  ├─ ConfigMap       (큐 이름, 설정 값)
  └─ Service         (헬스체크 / 메트릭)
```

즉, CRD 설계는 “사용자가 자주 같이 쓰는 리소스를 어떤 단위로 묶을 것인가”를 결정하는 일이다.

좋은 CRD 설계의 기준은 다음 세 가지로 정리할 수 있다.


| 기준  | 의미                                         |
| --- | ------------------------------------------ |
| 응집도 | 한 CR이 한 가지 운영 단위(예: 워커 1세트)를 책임진다          |
| 추상화 | 사용자는 “어떻게(how)”가 아닌 “무엇(what)”을 선언한다       |
| 안정성 | 한 번 공개된 spec 필드는 가볍게 바꾸지 않는다 (API 버전으로 관리) |


---

### 1-2. spec과 status의 역할 분리

> 공식 문서 (Kubernetes API Conventions, "spec and status"): "The `spec` field contains the desired state. The `status` field contains the observed state and is typically populated by the system."

이 규칙은 CRD 설계에서 가장 중요한 한 줄이다.

```text
spec
  - 사용자(또는 GitOps 도구)가 작성
  - "이렇게 되어야 한다"는 선언
  - Controller는 spec을 읽기만 한다 (수정 금지)

status
  - Controller(Operator)가 작성
  - "지금 실제로는 이렇다"는 관찰 결과
  - 사용자는 status를 읽기만 한다 (수정 권장하지 않음)
```

이 둘을 섞으면 Reconcile이 자기 자신이 쓴 값을 desired로 오해해 무한 루프가 생긴다.

```go
// MyAppSpec: 사용자가 선언하는 원하는 상태
type ImageWorkerSpec struct {
    // 워커 Pod 개수
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=20
    // +kubebuilder:default=1
    Replicas int32 `json:"replicas"`

    // 컨테이너 이미지
    // +kubebuilder:validation:Required
    Image string `json:"image"`

    // 처리할 큐 정보
    Queue QueueRef `json:"queue"`
}

// MyAppStatus: Controller가 기록하는 현재 상태
type ImageWorkerStatus struct {
    // 마지막으로 Reconcile이 반영한 spec generation
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`

    // Ready 상태인 Pod 수
    ReadyReplicas int32 `json:"readyReplicas,omitempty"`

    // 단계적 상태 (Pending / Ready / Degraded ...)
    Phase string `json:"phase,omitempty"`

    // 세부 상태 표현
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

`+kubebuilder:subresource:status` 마커를 붙이면 `/status` 서브리소스가 활성화되고, `r.Status().Update()` 호출은 spec을 건드리지 않는다.

```go
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type ImageWorker struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   ImageWorkerSpec   `json:"spec,omitempty"`
    Status ImageWorkerStatus `json:"status,omitempty"`
}
```

> 공식 문서 (Kubernetes Docs, "Extend the Kubernetes API with CustomResourceDefinitions" — Subresources): "Custom resources support `/status` and `/scale` subresources. Update operations to the `/status` subresource only update the status stanza."

즉, `subresource:status`는 단순한 옵션이 아니라 **spec/status 권한 분리의 기반**이다.

---

### 1-3. 검증과 기본값: 잘못된 spec을 API Server 단에서 막기

Reconcile이 잘못된 spec을 받지 않게 하려면 가능한 한 **API Server 단에서 검증**을 끝내야 한다.

> 공식 문서 (Kubebuilder Book, "CRD Validation"): "Kubebuilder uses structural schemas generated from Go markers to validate CRs at admission time."

자주 쓰는 마커는 다음과 같다.


| 마커                                             | 효과                 |
| ---------------------------------------------- | ------------------ |
| `+kubebuilder:validation:Required`             | 필수 필드              |
| `+kubebuilder:validation:Minimum/Maximum`      | 숫자 범위              |
| `+kubebuilder:validation:MinLength/MaxLength`  | 문자열 길이             |
| `+kubebuilder:validation:Pattern=`             | 정규식                |
| `+kubebuilder:validation:Enum={a,b,c}`         | 열거형                |
| `+kubebuilder:default=...`                     | 기본값                |
| `+kubebuilder:validation:XValidation:rule=...` | CEL 기반 규칙 (v1.25+) |


```go
type ImageWorkerSpec struct {
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:Pattern=`^.+:.+$`
    Image string `json:"image"`

    // +kubebuilder:validation:Enum=Standard;Spot
    // +kubebuilder:default=Standard
    NodeClass string `json:"nodeClass,omitempty"`

    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=20
    // +kubebuilder:default=1
    Replicas int32 `json:"replicas"`
}
```

이렇게 두면 잘못된 CR은 API Server가 거절하므로 **Reconcile에서 검증 코드를 줄일 수 있다**.

복잡한 검증(예: A 필드가 true면 B 필드는 반드시 있어야 함)은 **Validating Admission Webhook** 또는 **CEL Validation Rules**로 처리한다(Week5+ 주제).

---

### 1-4. API 버전 전략

CRD도 일반 Kubernetes API처럼 버전이 있다.

```text
v1alpha1  → 실험 단계, 깨질 수 있음
v1beta1   → 베타, 호환성 일부 보장
v1        → 안정, 공식 보장
```

운영 중 CR을 깨지 않고 spec 모양을 바꾸려면 **여러 버전을 동시에 서빙**하고, 그 사이를 **Conversion Webhook**으로 변환한다.

> 공식 문서 (Kubernetes Docs, "Versions in CustomResourceDefinitions"): "When you create a CustomResourceDefinition (CRD), you specify a set of versions. Conversion between versions is handled by a conversion webhook or the `None` strategy."

학습 단계에서는 다음만 잡으면 된다.

```text
첫 릴리스          → v1alpha1
구조 일부 변경     → v1alpha2 또는 v1beta1
안정화 후          → v1
필드 제거 / 이동   → 버전 올리고 Conversion Webhook 작성
```

핵심은 “이미 공개된 spec 필드를 함부로 의미만 바꾸지 않는다”는 것이다. 의미를 바꾸려면 새 버전을 추가한다.

---

## 2. 상태 머신 설계: status 전이 정의

### 2-1. status는 “현재 상태”의 단일 진실의 원천

Reconcile이 매번 다시 실행되어도 사용자와 다른 컨트롤러가 “지금 이 CR이 어떤 상태인가”를 한 곳에서 읽을 수 있어야 한다. 그 자리가 `status`다.

status에 일반적으로 넣는 정보는 다음과 같다.


| 필드                        | 의미                                           |
| ------------------------- | -------------------------------------------- |
| `observedGeneration`      | 마지막으로 Reconcile이 처리한 spec generation         |
| `phase` (선택)              | 사람이 읽기 쉬운 단계 표현                              |
| `conditions[]`            | 다축 상태 (Available / Progressing / Degraded …) |
| `readyReplicas`, `url`, … | 도메인별로 사용자에게 보여줄 값                            |


`observedGeneration`은 “지금 보이는 status가 어느 시점의 spec을 기준으로 한 것인가”를 알려준다.

```text
spec.generation = 5
status.observedGeneration = 4
  → 아직 spec 변경이 status에 반영되지 않음 (처리 중)

spec.generation = 5
status.observedGeneration = 5
  → 최신 spec까지 Reconcile이 따라잡음
```

---

### 2-2. phase vs conditions: 무엇을 쓰는가

상태를 단일 문자열(`phase`)로만 표현하면 “Ready인데 이미지 풀이 실패한 상태”처럼 동시에 여러 사실을 표현할 수 없다.

> 공식 문서 (Kubernetes API Conventions, "Typical status properties"): "Conditions are an extension mechanism intended to be used when the details of an observation are not a priori known or would not apply to all instances of a given Kind. ... New API definitions should prefer conditions over phases."

즉, 공식 권장은 다음과 같다.

```text
phase         : 사람이 빠르게 훑어볼 한 줄 (선택)
conditions[]  : 기계가 읽고, 자동화가 분기할 진짜 상태
```

자주 쓰는 Condition Type 예시는 다음과 같다.


| Type             | 의미                       |
| ---------------- | ------------------------ |
| `Ready`          | 사용자 입장에서 “쓸 수 있는가”       |
| `Available`      | 일부라도 정상 동작 중인가           |
| `Progressing`    | 변경을 반영하는 중인가             |
| `Degraded`       | 정상이지만 성능/리던던시 저하         |
| `ReconcileError` | 마지막 Reconcile에서 오류가 있었는가 |


`metav1.Condition`은 다음 6개 필드로 표준화돼 있다.

```go
type Condition struct {
    Type               string             // "Ready", "Degraded", ...
    Status             ConditionStatus    // True / False / Unknown
    ObservedGeneration int64              // 이 condition을 적은 시점의 generation
    LastTransitionTime metav1.Time
    Reason             string             // 기계가 읽는 CamelCase 이유 코드
    Message            string             // 사람이 읽는 설명
}
```

controller-runtime / apimachinery 표준 도구로 안전하게 갱신할 수 있다.

```go
import "k8s.io/apimachinery/pkg/api/meta"

meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
    Type:               "Ready",
    Status:             metav1.ConditionTrue,
    Reason:             "DeploymentAvailable",
    Message:            "Deployment has minimum availability.",
    ObservedGeneration: obj.Generation,
})
```

`SetStatusCondition`은 같은 `Type`이 이미 있으면 갱신하고, `Status`가 바뀌었을 때만 `LastTransitionTime`을 갱신한다. 직접 배열을 조작하지 말고 항상 이 헬퍼를 쓴다.

---

### 2-3. 상태 전이 그래프 정의

상태 머신은 “어떤 단계에서 어떤 단계로 갈 수 있는가”를 그려보고 시작한다.

예: ImageWorker가 처음 만들어져서 사용 가능 상태가 될 때까지의 전이.

```text
[Pending] ── 하위 리소스 생성 시작 ──► [Progressing]
                                          │
                              모든 Pod Ready
                                          ▼
                                       [Ready]
                                          │
                    노드 이슈 등으로 Ready 수 < spec
                                          ▼
                                     [Degraded]
                                          │
                                회복 시
                                          ▼
                                       [Ready]

CR 삭제 시 어디서든 ──► [Terminating] ──► (오브젝트 삭제 완료)
```

이 그래프를 Condition으로 옮기면 다음과 같다.


| 단계          | Available | Progressing | Degraded |
| ----------- | --------- | ----------- | -------- |
| Pending     | False     | True        | False    |
| Progressing | False     | True        | False    |
| Ready       | True      | False       | False    |
| Degraded    | True      | False       | True     |
| Terminating | False     | False       | False    |


핵심 규칙은 다음과 같다.

```text
1. 모든 단계는 Conditions의 조합으로 표현할 수 있어야 한다.
2. Reconcile은 매번 "현재 단계가 무엇인가"를 처음부터 다시 계산해야 한다.
   (이전 status를 누적하지 말고, 관찰된 사실로 다시 채운다)
3. 같은 입력이면 같은 status가 나와야 한다 (멱등성).
```

---

## 3. 리소스 관계: Dependency 처리

### 3-1. 주 리소스와 하위 리소스

Operator가 다루는 리소스는 두 종류로 나뉜다.

```text
주 리소스 (Primary)
  - Operator가 정의한 CRD의 CR (예: ImageWorker)
  - Reconcile의 진입점이 되는 리소스

하위 리소스 (Secondary / Owned)
  - 주 리소스 1개를 위해 Operator가 만들어 주는 Kubernetes 리소스
  - 예: Deployment, Service, ConfigMap, PVC
```

이 둘의 관계는 다음 규칙을 따른다.

```text
주 리소스 (ImageWorker)
   │
   │ ownerReference (controller=true)
   ▼
하위 리소스 (Deployment, Service, ConfigMap)
```

이 OwnerReference는 단순한 메타데이터가 아니라 **GC 동작과 Reconcile 트리거 둘 다**를 결정한다(자세한 내용은 `week4-reconcile.md` §4).

---

### 3-2. Watch 관계 등록: For / Owns / Watches

`controller-runtime`은 `Builder`로 어떤 리소스의 변화를 Reconcile 트리거로 쓸지를 선언한다.

```go
func (r *ImageWorkerReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&appsv1alpha1.ImageWorker{}).      // 주 리소스
        Owns(&appsv1.Deployment{}).            // 내가 만든 하위 리소스
        Owns(&corev1.Service{}).
        Owns(&corev1.ConfigMap{}).
        Watches(                                // 외부 의존성
            &corev1.Secret{},
            handler.EnqueueRequestsFromMapFunc(r.findImageWorkersForSecret),
        ).
        Complete(r)
}
```


| 메서드         | 의미                        | 트리거                                                |
| ----------- | ------------------------- | -------------------------------------------------- |
| `For()`     | 주 CR                      | CR 자체 변경                                           |
| `Owns()`    | OwnerReference로 묶인 하위 리소스 | 하위 리소스 변경 시 “owner의 namespace/name”으로 Reconcile 요청 |
| `Watches()` | 소유 관계가 아닌 외부 리소스          | `MapFunc`이 결정한 CR로 Reconcile 요청                    |


> 공식 문서 (controller-runtime godoc, `Builder.Owns`): "Owns defines types of Objects being generated by the ControllerManagedBy, and configures the ControllerManagedBy to respond to create / delete / update events by reconciling the owner object."

즉, `Owns()`는 **OwnerReference가 가리키는 주 리소스 이름으로 자동 enqueue**해 준다. 그래서 하위 리소스가 누가 손대 바뀌어도 다음 Reconcile에서 자동으로 “원하는 상태”로 복구된다.

---

### 3-3. 외부 의존성: `Watches` + MapFunc

“내가 만든 건 아니지만, 바뀌면 내 Reconcile에 영향을 주는 리소스”가 있다. 대표적으로 사용자가 직접 만든 `Secret` / `ConfigMap` / 외부 CR 등이다.

이때는 `Owns()`가 아니라 `Watches()`로 등록하고, 그 리소스 → 영향 받는 CR 목록을 매핑하는 함수를 준다.

```go
func (r *ImageWorkerReconciler) findImageWorkersForSecret(
    ctx context.Context, obj client.Object,
) []reconcile.Request {
    secret := obj.(*corev1.Secret)
    var list appsv1alpha1.ImageWorkerList

    // 같은 namespace에서 secret을 참조하는 CR 찾기
    if err := r.List(ctx, &list, client.InNamespace(secret.Namespace)); err != nil {
        return nil
    }

    var reqs []reconcile.Request
    for _, iw := range list.Items {
        if iw.Spec.Queue.CredentialsSecret == secret.Name {
            reqs = append(reqs, reconcile.Request{
                NamespacedName: types.NamespacedName{
                    Namespace: iw.Namespace, Name: iw.Name,
                },
            })
        }
    }
    return reqs
}
```

핵심은 단순하다.

```text
이벤트 (외부 리소스 변경)
  → MapFunc이 "이 변경이 영향을 주는 CR들"을 계산
  → 각 CR로 reconcile.Request 생성
  → Workqueue에 enqueue
```

이 패턴 덕분에 Operator는 OwnerReference가 없는 리소스 변화에도 안정적으로 반응할 수 있다.

---

## 4. 정리


| 주제                 | 핵심 정리                                                          |
| ------------------ | -------------------------------------------------------------- |
| 리소스 모델링            | 한 CR이 한 운영 단위를 책임지도록 묶고, “how”가 아닌 “what”을 선언하게 한다             |
| spec / status 분리   | spec = 사용자, status = Controller. `subresource:status`로 권한 분리   |
| 검증 / 기본값           | kubebuilder validation 마커로 잘못된 spec을 API Server 단에서 거절한다       |
| API 버전             | 의미가 바뀌면 새 버전을 만든다. 같은 버전에서 필드 의미를 함부로 바꾸지 않는다                  |
| 상태 머신              | `phase`보다 `conditions[]`를 우선. 모든 단계는 condition 조합으로 표현 가능해야 한다 |
| observedGeneration | “이 status가 어떤 spec에 대한 관찰인가”를 항상 같이 적는다                        |
| 리소스 관계             | 주 리소스 = `For`, 내가 만든 하위 = `Owns`, 외부 의존 = `Watches` + MapFunc  |


이 문서의 결과물은 `api/.../types.go`에 거의 그대로 반영된다.

```text
api/.../types.go
  ├─ Spec 구조체  (kubebuilder 마커 포함)
  ├─ Status 구조체 (observedGeneration + conditions[])
  └─ Root 타입에 +kubebuilder:subresource:status
```

이 모양이 잡힌 다음, 다음 문서에서 `internal/controller/..._controller.go`의 Reconcile을 구현한다.

---

## 참고 공식 문서

- [Kubernetes API Conventions — spec and status](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status)
- [Kubernetes API Conventions — Typical status properties (Conditions)](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties)
- [Extend the Kubernetes API with CustomResourceDefinitions](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/)
- [CustomResourceDefinitions — Status subresource](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#status-subresource)
- [Versions in CustomResourceDefinitions](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#versions)
- [CRD Validation Rules (CEL)](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-validation-rules/)
- [Kubebuilder Book — Designing APIs](https://book.kubebuilder.io/cronjob-tutorial/api-design.html)
- [Kubebuilder Book — Markers for Config/Code Generation](https://book.kubebuilder.io/reference/markers.html)
- [controller-runtime — Builder (For/Owns/Watches)](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/builder)

