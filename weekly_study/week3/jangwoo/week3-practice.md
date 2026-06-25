# Week 3 - 실습

> Week3 범위: SDK/Kubebuilder 사용 이유, Scaffolding 구조, controller-runtime 연결 구조 확인
> Week4 범위: Reconcile loop 구현, 리소스 생성/조회/업데이트, OwnerReference, status 업데이트

---

## 1. 프로젝트 구조 확인

```text
 tree -L .2
tree: Invalid level, must be greater than 0.
 ✘ Jangwoo 💡   ~/Desktop/Study/k8s-study
 tree -L 2 .
.
├── AGENTS.md
├── api
│   └── v1alpha1
├── bin
│   ├── controller-gen -> /Users/jangwoojung/Desktop/Study/k8s-study/bin/controller-gen-v0.20.1
│   └── controller-gen-v0.20.1
├── cmd
│   └── main.go
├── config
│   ├── crd
│   ├── default
│   ├── manager
│   ├── network-policy
│   ├── prometheus
│   ├── rbac
│   └── samples
├── Dockerfile
├── go.mod
├── go.sum
├── hack
│   └── boilerplate.go.txt
├── internal
│   └── controller
├── Makefile
├── PROJECT
├── README.md
└── test
    ├── e2e
    └── utils

19 directories, 11 files
```

`tree -L .2`는 잘못된 옵션 값이라 실패했다. `-L`에는 `2`처럼 숫자를 넣어야 한다.

---

## 2. CRD 타입 작성

처음 작성한 `api/v1alpha1/myapp_types.go` 내용이다.

```go
package v1alpha1

import (
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// MyAppSpec defines the desired state of MyApp
type MyAppSpec struct {
        // +kubebuilder:validation:Minimum=1
        // +kubebuilder:validation:Maximum=5
        // +kubebuilder:default=1
        Replicas int32 `json:"replicas,omitempty"`
        // +kubebuilder:validation:Required
        Image string `json:"image"`
}

// MyAppStatus defines the observed state of MyApp.
type MyAppStatus struct {
        ReadyReplicas int32 `json:"readyReplicas,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type MyApp struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   MyAppSpec   `json:"spec,omitempty"`
    Status MyAppStatus `json:"status,omitempty"`
}

// MyAppList contains a list of MyApp
type MyAppList struct {
        metav1.TypeMeta `json:",inline"`
        metav1.ListMeta `json:"metadata,omitzero"`
        Items           []MyApp `json:"items"`
}

func init() {
        SchemeBuilder.Register(&MyApp{}, &MyAppList{})
}
```

이 상태에서는 뒤에서 `MyAppList` 관련 오류가 발생했다.

수정 후 `MyAppList`에는 아래 marker를 추가했다.

```go
// +kubebuilder:object:root=true
type MyAppList struct {
        metav1.TypeMeta `json:",inline"`
        metav1.ListMeta `json:"metadata,omitempty"`
        Items           []MyApp `json:"items"`
}
```

---

## 3. controller-gen 실행

```text
 Jangwoo 💡   ~/Desktop/Study/k8s-study
 vim api/v1alpha1/myapp_types.go



 make generate
"/Users/jangwoojung/Desktop/Study/k8s-study/bin/controller-gen" object:headerFile="hack/boilerplate.go.txt",year=2026 paths="./..."
 Jangwoo 💡   ~/Desktop/Study/k8s-study
 make manifests
"/Users/jangwoojung/Desktop/Study/k8s-study/bin/controller-gen" rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
```

생성된 CRD에서 확인한 핵심 부분이다.

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: myapps.apps.jangwoo.dev
spec:
  group: apps.jangwoo.dev
  names:
    kind: MyApp
    listKind: MyAppList
    plural: myapps
    singular: myapp
  scope: Namespaced
  versions:
  - additionalPrinterColumns:
    - jsonPath: .spec.replicas
      name: Replicas
      type: integer
    - jsonPath: .status.readyReplicas
      name: Ready
      type: integer
    - jsonPath: .metadata.creationTimestamp
      name: Age
      type: date
    schema:
      openAPIV3Schema:
        properties:
          spec:
            properties:
              image:
                type: string
              replicas:
                default: 1
                format: int32
                maximum: 5
                minimum: 1
                type: integer
            required:
            - image
            type: object
          status:
            properties:
              readyReplicas:
                format: int32
                type: integer
            type: object
    subresources:
      status: {}
```

---

## 4. CRD 설치 확인

```text
 Jangwoo 💡   ~/Desktop/Study/k8s-study
 kubectl get crd myapps.apps.jangwoo.dev
NAME                      CREATED AT
myapps.apps.jangwoo.dev   2026-05-21T11:15:17Z
 Jangwoo 💡   ~/Desktop/Study/k8s-study
 kubectl explain myapp.spec
GROUP:      apps.jangwoo.dev
KIND:       MyApp
VERSION:    v1alpha1

FIELD: spec <Object>


DESCRIPTION:
    MyAppSpec defines the desired state of MyApp

FIELDS:
  image	<string> -required-
    <no description>

  replicas	<integer>
    <no description>
```

---

## 5. 오류 기록: MyAppList DeepCopyObject 누락

```text
 Jangwoo 💡   ~/Desktop/Study/k8s-study
 make run
"/Users/jangwoojung/Desktop/Study/k8s-study/bin/controller-gen" rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
"/Users/jangwoojung/Desktop/Study/k8s-study/bin/controller-gen" object:headerFile="hack/boilerplate.go.txt",year=2026 paths="./..."
go fmt ./...
api/v1alpha1/myapp_types.go
go vet ./...
# github.com/jangwoojung/test-operator/api/v1alpha1
api/v1alpha1/myapp_types.go:62:35: cannot use &MyAppList{} (value of type *MyAppList) as "k8s.io/apimachinery/pkg/runtime".Object value in argument to SchemeBuilder.Register: *MyAppList does not implement "k8s.io/apimachinery/pkg/runtime".Object (missing method DeepCopyObject)
make: *** [vet] Error 1
 ✘ Jangwoo 💡   ~/Desktop/Study/k8s-study
```

원인: `SchemeBuilder.Register(&MyApp{}, &MyAppList{})`에 등록되는 타입은 `runtime.Object`를 구현해야 한다. `runtime.Object` 구현에 필요한 `DeepCopyObject()`는 `controller-gen`이 생성한다.

오류 당시 `MyApp`에는 `// +kubebuilder:object:root=true`가 있었지만, `MyAppList`에는 없었다. 그래서 `controller-gen`이 `MyAppList.DeepCopyObject()`를 만들지 않았고 `go vet`에서 실패했다.

수정: `MyAppList`에도 `// +kubebuilder:object:root=true`를 추가하고 `make generate`를 다시 실행했다.

---

## 6. make run 성공

```text
 make run
"/Users/jangwoojung/Desktop/Study/k8s-study/bin/controller-gen" rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
"/Users/jangwoojung/Desktop/Study/k8s-study/bin/controller-gen" object:headerFile="hack/boilerplate.go.txt",year=2026 paths="./..."
go fmt ./...
go vet ./...
go run ./cmd/main.go
2026-05-21T20:36:07+09:00	INFO	setup	Starting manager
2026-05-21T20:36:07+09:00	INFO	starting server	{"name": "health probe", "addr": "[::]:8081"}
2026-05-21T20:36:07+09:00	INFO	Starting EventSource	{"controller": "myapp", "controllerGroup": "apps.jangwoo.dev", "controllerKind": "MyApp", "source": "kind source: *v1alpha1.MyApp"}
2026-05-21T20:36:07+09:00	INFO	Starting Controller	{"controller": "myapp", "controllerGroup": "apps.jangwoo.dev", "controllerKind": "MyApp"}
2026-05-21T20:36:07+09:00	INFO	Starting workers	{"controller": "myapp", "controllerGroup": "apps.jangwoo.dev", "controllerKind": "MyApp", "worker count": 1}
```

`go vet` 통과 후 Manager와 MyApp Controller가 정상 시작되었다.

---

## 7. Custom Resource 생성 확인

```text
 Jangwoo 🔑   ~/Desktop/Study/k8s-study
 kubectl apply -f config/samples/apps_v1alpha1_myapp.yaml
myapp.apps.jangwoo.dev/sample-myapp created
 Jangwoo 🔑   ~/Desktop/Study/k8s-study
 kubectl get myapp
kubectl get myapp sample-myapp -o yaml 
NAME           REPLICAS   READY   AGE
sample-myapp   2                  6s
apiVersion: apps.jangwoo.dev/v1alpha1
kind: MyApp
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"apps.jangwoo.dev/v1alpha1","kind":"MyApp","metadata":{"annotations":{},"labels":{"app.kubernetes.io/managed-by":"kustomize","app.kubernetes.io/name":"jangwoo-my-operator"},"name":"sample-myapp","namespace":"default"},"spec":{"image":"nginx:1.25","replicas":2}}
  creationTimestamp: "2026-05-21T11:37:03Z"
  generation: 1
  labels:
    app.kubernetes.io/managed-by: kustomize
    app.kubernetes.io/name: jangwoo-my-operator
  name: sample-myapp
  namespace: default
  resourceVersion: "422934"
  uid: 538b549a-b2f3-4bf1-aabb-aca68a4fb63e
spec:
  image: nginx:1.25
  replicas: 2
```

`READY`가 비어 있는 것은 정상이다. Week3에서는 status를 갱신하는 Reconcile 로직을 구현하지 않았다.

---

## 8. CRD validation 확인

```text
 Jangwoo 🔑   ~/Desktop/Study/k8s-study
 kubectl patch myapp sample-myapp --type=merge -p '{"spec":{"replicas":10}}'
The MyApp "sample-myapp" is invalid: spec.replicas: Invalid value: 10: spec.replicas in body should be less than or equal to 5
 ✘ Jangwoo 🔑   ~/Desktop/Study/k8s-study
 
```

`replicas`에 `Maximum=5` marker를 넣었기 때문에 API Server가 요청을 거절했다. 이 검증은 Controller가 아니라 CRD 스키마에서 수행된다.

---

## 9. 정리

Week3에서 확인한 것:

```text
Kubebuilder scaffolding
  -> CRD Go 타입 작성
  -> controller-gen으로 CRD/RBAC/DeepCopy 생성
  -> CRD 설치
  -> Manager와 Controller 시작
  -> Custom Resource 생성
```

Week4에서 할 것:

```text
Reconcile loop 구현
  -> 리소스 생성/조회/업데이트
  -> OwnerReference
  -> status 업데이트
```
