# Kubernetes Operator의 Reconcile 흐름 정리

## 1. Reconcile 함수의 역할

`Reconcile` 함수는 Kubernetes Operator의 핵심 로직을 담당했다.

사용자가 만든 Custom Resource의 원하는 상태와 현재 Kubernetes 클러스터의 실제 상태를 비교하고, 두 상태가 다르면 실제 상태를 원하는 상태로 맞추는 역할을 했다.

즉, `Reconcile` 함수는 단순히 리소스를 생성하는 함수가 아니라, Kubernetes 리소스의 상태를 계속 확인하고 복구하는 반복 루프 역할을 했다.

```text
사용자가 Custom Resource 생성 또는 수정
        ↓
Controller가 이벤트 감지
        ↓
Reconcile 함수 실행
        ↓
Custom Resource 조회
        ↓
원하는 하위 리소스 정의
        ↓
현재 하위 리소스 조회
        ↓
없으면 생성
        ↓
있으면 상태 비교 후 업데이트
        ↓
OwnerReference로 소유 관계 설정
        ↓
정상 종료 또는 재실행 예약
```

---

## 2. Reconcile 함수의 기본 구조

아래 코드는 `MyApp`이라는 Custom Resource를 기준으로 Deployment를 생성하고 관리하는 예시였다.

```go
func (r *MyAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := log.FromContext(ctx)

    // 1. Custom Resource 조회
    myApp := &appv1.MyApp{}
    err := r.Get(ctx, req.NamespacedName, myApp)
    if err != nil {
        if apierrors.IsNotFound(err) {
            // CR이 삭제된 경우 정상 종료했다.
            return ctrl.Result{}, nil
        }
        return ctrl.Result{}, err
    }

    // 2. 원하는 하위 리소스 정의
    desiredDeployment := r.deploymentForMyApp(myApp)

    // 3. OwnerReference 설정
    if err := ctrl.SetControllerReference(myApp, desiredDeployment, r.Scheme); err != nil {
        return ctrl.Result{}, err
    }

    // 4. 실제 리소스 조회
    existingDeployment := &appsv1.Deployment{}
    err = r.Get(ctx, types.NamespacedName{
        Name:      desiredDeployment.Name,
        Namespace: desiredDeployment.Namespace,
    }, existingDeployment)

    // 5. 없으면 생성
    if apierrors.IsNotFound(err) {
        log.Info("Creating Deployment", "name", desiredDeployment.Name)

        if err := r.Create(ctx, desiredDeployment); err != nil {
            return ctrl.Result{}, err
        }

        return ctrl.Result{}, nil
    }

    if err != nil {
        return ctrl.Result{}, err
    }

    // 6. 있으면 현재 상태와 원하는 상태 비교 후 업데이트
    if existingDeployment.Spec.Replicas == nil ||
        *existingDeployment.Spec.Replicas != *desiredDeployment.Spec.Replicas {

        existingDeployment.Spec.Replicas = desiredDeployment.Spec.Replicas

        if err := r.Update(ctx, existingDeployment); err != nil {
            return ctrl.Result{}, err
        }
    }

    // 7. 정상 종료
    return ctrl.Result{}, nil
}
```

---

## 3. `ctx context.Context`의 의미

`ctx`는 Kubernetes API 요청을 실행할 때 함께 전달되는 작업 흐름 정보였다.

`ctx`에는 작업 취소 여부, 시간 제한, 로그, 트레이싱 정보 등이 포함될 수 있었다.

그래서 Kubernetes API를 호출할 때 `Get`, `Create`, `Update`와 함께 전달했다.

```go
r.Get(ctx, req.NamespacedName, myApp)
r.Create(ctx, desiredDeployment)
r.Update(ctx, existingDeployment)
```

초보자 입장에서는 `ctx`를 Kubernetes API 요청을 수행할 때 함께 넘기는 실행 문맥 정보로 이해했다.

---

## 4. `req ctrl.Request`의 의미

`req`는 어떤 리소스 때문에 `Reconcile` 함수가 호출되었는지 알려주는 요청 정보였다.

보통 `req.NamespacedName`을 통해 변경된 리소스의 이름과 네임스페이스를 알 수 있었다.

```go
req.NamespacedName
```

예를 들면 아래와 같은 정보를 담고 있었다.

```text
Namespace: default
Name: sample-app
```

그래서 아래 코드는 방금 변경된 `MyApp` 리소스를 조회하는 의미를 가졌다.

```go
r.Get(ctx, req.NamespacedName, myApp)
```

즉, `req.NamespacedName`은 Reconcile 대상이 되는 Custom Resource의 위치 정보 역할을 했다.

---

## 5. Custom Resource 조회 로직

Reconcile 함수는 가장 먼저 Custom Resource를 조회했다.

```go
myApp := &appv1.MyApp{}

err := r.Get(ctx, req.NamespacedName, myApp)
if err != nil {
    if apierrors.IsNotFound(err) {
        return ctrl.Result{}, nil
    }
    return ctrl.Result{}, err
}
```

여기서 `r.Get()`은 Kubernetes API Server를 통해 현재 클러스터에 저장된 리소스를 조회했다.

흐름은 아래와 같았다.

```text
Reconcile 함수
   ↓
controller-runtime client
   ↓
Kubernetes API Server
   ↓
etcd에 저장된 리소스 조회
```

따라서 `r.Get()`은 단순히 코드 내부의 값을 읽는 것이 아니라, 실제 Kubernetes 클러스터 상태를 조회하는 동작이었다.

---

## 6. `apierrors.IsNotFound(err)` 처리가 필요한 이유

Custom Resource가 삭제되었을 때도 Reconcile 함수가 호출될 수 있었다.

그런데 이미 삭제된 리소스를 다시 조회하면 `NotFound` 에러가 발생했다.

이때는 실제 장애 상황이 아니라, 리소스가 이미 삭제된 정상 상황으로 봐야 했다.

```go
if apierrors.IsNotFound(err) {
    return ctrl.Result{}, nil
}
```

흐름은 아래와 같았다.

```text
Custom Resource 삭제
        ↓
Reconcile 호출
        ↓
r.Get()으로 조회
        ↓
이미 삭제되어 NotFound 발생
        ↓
정상 종료
```

따라서 `IsNotFound`인 경우에는 에러를 반환하지 않고 `nil`로 종료했다.

만약 이 상황에서 에러를 그대로 반환하면 컨트롤러가 실패로 판단하고 불필요하게 재시도할 수 있었다.

---

## 7. 원하는 하위 리소스 정의

Custom Resource를 조회한 뒤에는 이 CR을 기준으로 원하는 하위 리소스를 정의했다.

```go
desiredDeployment := r.deploymentForMyApp(myApp)
```

여기서 `desiredDeployment`는 실제 클러스터에 존재하는 Deployment가 아니라, CR을 기준으로 만들어낸 목표 상태였다.

예를 들어 Custom Resource가 아래와 같다고 가정했다.

```yaml
apiVersion: app.example.com/v1
kind: MyApp
metadata:
  name: sample-app
spec:
  replicas: 3
  image: nginx:1.25
```

그러면 Operator는 이 정보를 바탕으로 Deployment가 어떻게 존재해야 하는지 정의했다.

```go
func (r *MyAppReconciler) deploymentForMyApp(myApp *appv1.MyApp) *appsv1.Deployment {
    labels := map[string]string{
        "app": myApp.Name,
    }

    replicas := myApp.Spec.Replicas

    return &appsv1.Deployment{
        ObjectMeta: metav1.ObjectMeta{
            Name:      myApp.Name + "-deployment",
            Namespace: myApp.Namespace,
            Labels:    labels,
        },
        Spec: appsv1.DeploymentSpec{
            Replicas: &replicas,
            Selector: &metav1.LabelSelector{
                MatchLabels: labels,
            },
            Template: corev1.PodTemplateSpec{
                ObjectMeta: metav1.ObjectMeta{
                    Labels: labels,
                },
                Spec: corev1.PodSpec{
                    Containers: []corev1.Container{
                        {
                            Name:  "app",
                            Image: myApp.Spec.Image,
                            Ports: []corev1.ContainerPort{
                                {
                                    ContainerPort: 80,
                                },
                            },
                        },
                    },
                },
            },
        },
    }
}
```

이 함수는 Deployment를 바로 생성하는 함수가 아니었다.

이 함수는 사용자가 CR에 작성한 값을 기준으로 원하는 Deployment 상태를 코드로 만든 함수였다.

---

## 8. `desiredDeployment`와 `existingDeployment`의 차이

Operator에서 가장 중요한 개념은 원하는 상태와 현재 상태를 비교하는 것이었다.

```go
desiredDeployment := r.deploymentForMyApp(myApp)
```

`desiredDeployment`는 사용자가 CR에 작성한 값을 기준으로 만든 원하는 상태였다.

```go
existingDeployment := &appsv1.Deployment{}
err = r.Get(ctx, types.NamespacedName{
    Name:      desiredDeployment.Name,
    Namespace: desiredDeployment.Namespace,
}, existingDeployment)
```

`existingDeployment`는 현재 Kubernetes 클러스터에 실제로 존재하는 Deployment 상태였다.

정리하면 아래와 같았다.

```text
desiredDeployment
= Custom Resource를 기준으로 만든 목표 상태였다.

existingDeployment
= 현재 클러스터에 실제로 존재하는 상태였다.
```

Operator의 핵심은 이 둘을 비교하는 것이었다.

```text
원하는 상태와 실제 상태가 같음
        ↓
아무 작업도 하지 않고 종료했다.

원하는 상태와 실제 상태가 다름
        ↓
실제 상태를 원하는 상태로 업데이트했다.
```

---

## 9. 실제 리소스 조회 로직

원하는 Deployment를 정의한 뒤에는 실제 클러스터에 해당 Deployment가 존재하는지 조회했다.

```go
existingDeployment := &appsv1.Deployment{}

err = r.Get(ctx, types.NamespacedName{
    Name:      desiredDeployment.Name,
    Namespace: desiredDeployment.Namespace,
}, existingDeployment)
```

조회 결과는 크게 세 가지로 나뉘었다.

```text
1. NotFound 발생
   → Deployment가 아직 존재하지 않았다.
   → Create를 수행했다.

2. 다른 에러 발생
   → API Server 문제, 권한 문제, 네트워크 문제 등이었다.
   → 에러를 반환했다.

3. 에러 없음
   → Deployment가 이미 존재했다.
   → 원하는 상태와 현재 상태를 비교했다.
```

---

## 10. 리소스 생성 로직

Deployment가 존재하지 않으면 새로 생성했다.

```go
if apierrors.IsNotFound(err) {
    log.Info("Creating Deployment", "name", desiredDeployment.Name)

    if err := r.Create(ctx, desiredDeployment); err != nil {
        return ctrl.Result{}, err
    }

    return ctrl.Result{}, nil
}
```

여기서 `r.Create()`는 Kubernetes API Server에 Deployment 생성 요청을 보내는 코드였다.

Deployment 생성이 성공하면 `return ctrl.Result{}, nil`로 정상 종료했다.

생성 후에 바로 `Requeue: true`를 하지 않는 이유는 Kubernetes 내부 리소스의 생성 이벤트가 다시 발생할 수 있기 때문이었다.

```text
Deployment 생성
        ↓
Kubernetes API Server에 저장
        ↓
Deployment 생성 이벤트 발생
        ↓
Owns 설정이 되어 있으면 Reconcile 재호출 가능
```

따라서 Kubernetes 내부 리소스를 생성한 뒤에는 보통 정상 종료하는 방식으로 처리했다.

---

## 11. 리소스 업데이트 로직

Deployment가 이미 존재하면 현재 상태와 원하는 상태를 비교했다.

```go
if existingDeployment.Spec.Replicas == nil ||
    *existingDeployment.Spec.Replicas != *desiredDeployment.Spec.Replicas {

    existingDeployment.Spec.Replicas = desiredDeployment.Spec.Replicas

    if err := r.Update(ctx, existingDeployment); err != nil {
        return ctrl.Result{}, err
    }
}
```

위 코드는 현재 Deployment의 `replicas` 값이 원하는 `replicas` 값과 다르면 업데이트하는 로직이었다.

즉, Custom Resource에 `replicas: 3`이라고 되어 있는데 실제 Deployment가 `replicas: 1`이면 Deployment를 수정했다.

```text
CR의 replicas 값
        ↓
사용자가 원하는 상태였다.

Deployment의 replicas 값
        ↓
현재 클러스터의 실제 상태였다.

두 값이 다름
        ↓
Deployment를 업데이트했다.
```

---

## 12. 왜 매번 Update하지 않고 비교 후 Update하는지

초보자는 매번 `Update()`를 호출하면 된다고 생각할 수 있었다.

하지만 매번 Update를 호출하면 불필요한 이벤트가 계속 발생할 수 있었다.

```text
Reconcile 실행
        ↓
무조건 Update 수행
        ↓
Deployment 변경 이벤트 발생
        ↓
Reconcile 다시 호출
        ↓
또 무조건 Update 수행
        ↓
불필요한 반복 발생
```

그래서 Operator에서는 현재 상태와 원하는 상태를 비교한 뒤, 실제로 다를 때만 업데이트하는 방식이 중요했다.

```text
같으면 아무 작업도 하지 않았다.
다르면 업데이트했다.
```

이 방식이 불필요한 Reconcile 반복을 줄이고 안정적인 컨트롤러 동작을 만들었다.

---

## 13. 이미지 변경까지 반영하는 업데이트 예시

현재 예시 코드는 `replicas`만 비교했다.

하지만 실제 Operator에서는 이미지 값도 변경될 수 있었다.

예를 들어 Custom Resource의 이미지가 아래처럼 바뀔 수 있었다.

```yaml
spec:
  image: nginx:1.26
```

이 경우 Deployment의 컨테이너 이미지도 업데이트되어야 했다.

```go
currentImage := existingDeployment.Spec.Template.Spec.Containers[0].Image
desiredImage := desiredDeployment.Spec.Template.Spec.Containers[0].Image

if currentImage != desiredImage {
    existingDeployment.Spec.Template.Spec.Containers[0].Image = desiredImage

    if err := r.Update(ctx, existingDeployment); err != nil {
        return ctrl.Result{}, err
    }
}
```

이처럼 Reconcile 함수는 사용자가 CR에 작성한 값이 변경되었을 때, 실제 하위 리소스도 그 값에 맞게 수정했다.

---

## 14. OwnerReference의 의미

`OwnerReference`는 Kubernetes 리소스 간의 부모와 자식 관계를 설정하는 기능이었다.

예를 들어 `MyApp` Custom Resource가 Deployment를 생성했다면 아래와 같은 관계가 만들어졌다.

```text
MyApp Custom Resource
        ↓
Deployment
        ↓
ReplicaSet
        ↓
Pod
```

이때 `MyApp`은 부모 리소스였고, Deployment는 자식 리소스였다.

OwnerReference는 이 관계를 Kubernetes에 알려주는 설정이었다.

```go
if err := ctrl.SetControllerReference(myApp, desiredDeployment, r.Scheme); err != nil {
    return ctrl.Result{}, err
}
```

이 코드는 `myApp`이 `desiredDeployment`를 소유한다는 의미를 가졌다.

---

## 15. OwnerReference를 설정하는 이유

OwnerReference를 설정하는 가장 큰 이유는 부모 리소스가 삭제되었을 때 자식 리소스도 함께 정리되도록 하기 위해서였다.

예를 들어 사용자가 아래 명령어로 `MyApp`을 삭제했다고 가정했다.

```bash
kubectl delete myapp sample-app
```

OwnerReference가 설정되어 있으면 Kubernetes Garbage Collector가 하위 Deployment도 자동으로 정리했다.

```text
MyApp 삭제
        ↓
OwnerReference 확인
        ↓
Deployment도 자동 삭제
```

OwnerReference가 없으면 Custom Resource는 삭제되었는데 Deployment는 남아 있을 수 있었다.

```text
MyApp 삭제
        ↓
Deployment는 그대로 남음
        ↓
불필요한 리소스가 계속 실행됨
```

따라서 Operator가 생성한 하위 리소스에는 OwnerReference를 설정하는 것이 중요했다.

---

## 16. `Owns()`의 의미

OwnerReference를 설정했다고 해서 하위 리소스 변경 이벤트가 자동으로 Reconcile에 연결되는 것은 아니었다.

컨트롤러 설정에서 `Owns()`를 함께 지정해야 했다.

```go
func (r *MyAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&appv1.MyApp{}).
        Owns(&appsv1.Deployment{}).
        Complete(r)
}
```

각 코드의 의미는 아래와 같았다.

```go
For(&appv1.MyApp{})
```

이 코드는 `MyApp`을 메인으로 감시하겠다는 의미였다.

```go
Owns(&appsv1.Deployment{})
```

이 코드는 `MyApp`이 소유한 Deployment도 감시하겠다는 의미였다.

즉, 사용자가 Deployment를 실수로 삭제해도 Operator가 이를 감지하고 다시 생성할 수 있었다.

```text
Deployment 삭제
        ↓
Owns 설정으로 이벤트 감지
        ↓
Reconcile 다시 호출
        ↓
Deployment가 없다고 판단
        ↓
Deployment 재생성
```

---

## 17. `ctrl.Result{}`의 의미

`ctrl.Result{}`는 Reconcile 함수가 끝난 뒤, 이 요청을 다시 실행할지 말지 controller-runtime에게 알려주는 값이었다.

Reconcile 함수는 아래처럼 두 개의 값을 반환했다.

```go
func (r *MyAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    ...
}
```

반환값은 아래 두 가지였다.

```text
ctrl.Result
= Reconcile을 다시 실행할지 결정하는 값이었다.

error
= 이번 Reconcile이 성공했는지 실패했는지 알려주는 값이었다.
```

즉, `ctrl.Result`는 다음 실행 스케줄에 가까웠고, `error`는 성공과 실패 여부에 가까웠다.

---

## 18. `return ctrl.Result{}, nil`

가장 기본적인 정상 종료 방식은 아래와 같았다.

```go
return ctrl.Result{}, nil
```

이 코드는 아래 의미를 가졌다.

```text
이번 Reconcile은 정상적으로 끝났다.
직접 재실행을 예약하지 않았다.
```

즉, 아무 에러도 없고 다시 실행할 필요도 없을 때 사용했다.

예를 들어 현재 Deployment 상태가 이미 원하는 상태와 같다면 아래처럼 종료했다.

```go
return ctrl.Result{}, nil
```

이 상황은 아래와 같았다.

```text
Custom Resource에는 replicas: 3
실제 Deployment도 replicas: 3
        ↓
더 이상 할 작업 없음
        ↓
정상 종료
```

---

## 19. `return ctrl.Result{}, err`

에러가 발생했을 때는 아래처럼 반환했다.

```go
return ctrl.Result{}, err
```

이 코드는 아래 의미를 가졌다.

```text
이번 Reconcile은 실패했다.
controller-runtime이 나중에 다시 시도할 수 있었다.
```

예를 들어 Deployment 생성에 실패하면 아래처럼 처리했다.

```go
if err := r.Create(ctx, desiredDeployment); err != nil {
    return ctrl.Result{}, err
}
```

흐름은 아래와 같았다.

```text
Deployment 생성 시도
        ↓
권한 문제 또는 API Server 문제 발생
        ↓
error 반환
        ↓
컨트롤러가 실패로 판단
        ↓
재시도 가능
```

중요한 점은 `ctrl.Result{}`가 비어 있어도 `err`가 `nil`이 아니면 실패로 처리된다는 점이었다.

---

## 20. `return ctrl.Result{Requeue: true}, nil`

즉시 다시 실행하고 싶을 때는 아래처럼 작성했다.

```go
return ctrl.Result{Requeue: true}, nil
```

이 코드는 아래 의미를 가졌다.

```text
에러는 없었다.
하지만 이 Reconcile 요청을 다시 큐에 넣었다.
```

즉, 바로 다시 한 번 Reconcile을 실행하라는 의미였다.

```go
if needImmediateCheck {
    return ctrl.Result{Requeue: true}, nil
}
```

하지만 `Requeue: true`는 남발하면 안 되었다.

계속 즉시 재실행되면 컨트롤러가 불필요하게 바쁘게 돌 수 있었기 때문이다.

```text
Requeue: true 반환
        ↓
즉시 Reconcile 재실행
        ↓
또 Requeue: true 반환
        ↓
반복 발생
```

따라서 바로 다시 실행할 필요가 명확한 경우에만 사용하는 것이 좋았다.

---

## 21. `return ctrl.Result{RequeueAfter: time.Minute}, nil`

일정 시간 뒤 다시 실행하고 싶을 때는 `RequeueAfter`를 사용했다.

```go
return ctrl.Result{RequeueAfter: time.Minute}, nil
```

이 코드는 아래 의미를 가졌다.

```text
에러는 없었다.
1분 뒤에 이 Reconcile을 다시 실행하도록 예약했다.
```

예를 들어 외부 데이터베이스가 아직 생성 중이라면 아래처럼 작성할 수 있었다.

```go
if database.Status == "creating" {
    return ctrl.Result{RequeueAfter: time.Minute}, nil
}
```

흐름은 아래와 같았다.

```text
Reconcile 실행
        ↓
외부 데이터베이스 상태 확인
        ↓
아직 creating 상태
        ↓
1분 뒤 다시 확인하도록 예약
```

`RequeueAfter`는 외부 시스템의 상태를 주기적으로 확인할 때 자주 사용했다.

---

## 22. Requeue에서 말하는 외부 시스템의 의미

여기서 외부 시스템은 Kubernetes 클러스터 안의 기본 리소스가 아니라, Operator가 추가로 확인하거나 제어해야 하는 클러스터 바깥의 서비스나 시스템을 의미했다.

예시는 아래와 같았다.

| 외부 시스템    | Operator가 확인하는 내용          |
| --------- | -------------------------- |
| AWS RDS   | DB 인스턴스 생성이 완료되었는지 확인했다.   |
| AWS S3    | 버킷이 실제로 만들어졌는지 확인했다.       |
| AWS ELB   | 로드밸런서가 준비되었는지 확인했다.        |
| Route53   | DNS 레코드가 반영되었는지 확인했다.      |
| 외부 API 서버 | 특정 작업이 완료되었는지 확인했다.        |
| 사내 시스템    | 계정 생성, 권한 부여, 승인 상태를 확인했다. |
| 데이터베이스    | 마이그레이션 완료 여부를 확인했다.        |
| Jenkins   | 빌드가 완료되었는지 확인했다.           |

예를 들어 `MyApp` CR을 만들면 Operator가 AWS RDS까지 생성한다고 가정했다.

```yaml
apiVersion: app.example.com/v1
kind: MyApp
metadata:
  name: sample-app
spec:
  database:
    engine: mysql
    size: small
```

이 경우 Operator는 AWS API를 호출해서 RDS 생성 요청을 보낼 수 있었다.

하지만 RDS는 생성 요청 직후 바로 사용 가능한 상태가 되지 않았다.

```text
creating
   ↓
backing-up
   ↓
available
```

따라서 Reconcile에서는 아래처럼 처리할 수 있었다.

```go
if rdsStatus != "available" {
    return ctrl.Result{RequeueAfter: time.Minute}, nil
}
```

이 코드는 RDS가 아직 준비되지 않았으므로 1분 뒤 다시 상태를 확인하겠다는 의미였다.

---

## 23. Kubernetes 내부 리소스와 외부 시스템의 차이

Kubernetes 내부 리소스는 Kubernetes가 이벤트를 감지해줄 수 있었다.

예를 들어 Deployment, Service, ConfigMap, Secret, Pod 등이 이에 해당했다.

```text
Deployment 생성
        ↓
Kubernetes API Server에 저장
        ↓
Deployment 생성 이벤트 발생
        ↓
Owns 설정이 되어 있으면 Reconcile 재호출 가능
```

반면 외부 시스템은 Kubernetes가 상태 변화를 자동으로 알 수 없었다.

예를 들어 AWS RDS의 상태가 `creating`에서 `available`로 바뀌어도 Kubernetes가 자동으로 Operator에게 알려주지 않았다.

그래서 Operator가 `RequeueAfter`를 사용해 일정 시간 뒤 직접 다시 확인해야 했다.

```text
Kubernetes 내부 리소스
= Deployment, Service, ConfigMap, Secret, Pod 등이었다.
= Kubernetes 이벤트가 다시 알려줄 수 있었다.

외부 시스템
= AWS, GCP, Azure, DB, Jenkins, 외부 API 등이었다.
= Kubernetes가 자동으로 상태 변화를 알 수 없었다.
= RequeueAfter로 주기적으로 다시 확인해야 했다.
```

---

## 24. `ctrl.Result` 반환 패턴 정리

자주 사용하는 반환 패턴은 아래와 같았다.

| 코드                                                   | 의미                        | 사용 상황                            |
| ---------------------------------------------------- | ------------------------- | -------------------------------- |
| `return ctrl.Result{}, nil`                          | 정상 종료하고 재실행을 예약하지 않았다.    | 원하는 상태와 실제 상태가 같을 때 사용했다.        |
| `return ctrl.Result{}, err`                          | 에러를 반환하고 재시도 가능 상태로 만들었다. | API 호출 실패, 권한 문제, 생성 실패 등에 사용했다. |
| `return ctrl.Result{Requeue: true}, nil`             | 즉시 다시 실행하도록 요청했다.         | 바로 다시 확인해야 할 때 사용했다.             |
| `return ctrl.Result{RequeueAfter: time.Minute}, nil` | 일정 시간 뒤 다시 실행하도록 예약했다.    | 외부 시스템 상태를 주기적으로 확인할 때 사용했다.     |

초보자 입장에서는 아래 세 가지를 우선 기억하면 충분했다.

```go
return ctrl.Result{}, nil
```

```text
정상 종료를 의미했다.
```

```go
return ctrl.Result{}, err
```

```text
에러 발생과 재시도 가능성을 의미했다.
```

```go
return ctrl.Result{RequeueAfter: time.Minute}, nil
```

```text
1분 뒤 다시 실행을 의미했다.
```

---

## 25. Status 업데이트의 의미

실제 Operator에서는 `spec`만 보는 것이 아니라 `status`도 업데이트했다.

`spec`은 사용자가 원하는 상태였고, `status`는 컨트롤러가 관찰한 현재 상태였다.

```yaml
spec:
  replicas: 3
  image: nginx

status:
  availableReplicas: 2
  phase: Running
```

정리하면 아래와 같았다.

```text
spec
= 사용자가 작성한 원하는 상태였다.

status
= 컨트롤러가 확인한 실제 상태였다.
```

예를 들어 Deployment의 사용 가능한 Pod 수를 Custom Resource의 status에 반영할 수 있었다.

```go
myApp.Status.AvailableReplicas = existingDeployment.Status.AvailableReplicas

if err := r.Status().Update(ctx, myApp); err != nil {
    return ctrl.Result{}, err
}
```

즉, `spec`은 사용자가 작성하고, `status`는 Operator가 작성했다.

---

## 26. RBAC 권한의 필요성

Operator 코드가 맞아도 권한이 없으면 동작하지 않았다.

Deployment를 조회하고 생성하고 수정하려면 RBAC 권한이 필요했다.

```go
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
```

Custom Resource를 조회하려면 Custom Resource에 대한 권한도 필요했다.

```go
//+kubebuilder:rbac:groups=app.example.com,resources=myapps,verbs=get;list;watch;create;update;patch;delete
```

Status를 업데이트하려면 status 하위 리소스에 대한 권한도 필요했다.

```go
//+kubebuilder:rbac:groups=app.example.com,resources=myapps/status,verbs=get;update;patch
```

즉, Reconcile 함수 안에서 Kubernetes API를 호출하려면 그에 맞는 RBAC 권한이 반드시 필요했다.

---

## 27. DeepCopy의 의미

Kubernetes 객체를 수정할 때는 원본 객체를 직접 수정하기보다 복사본을 만들어 수정하는 경우가 많았다.

```go
updated := existingDeployment.DeepCopy()
updated.Spec.Replicas = desiredDeployment.Spec.Replicas

if err := r.Update(ctx, updated); err != nil {
    return ctrl.Result{}, err
}
```

`DeepCopy()`는 Kubernetes 객체를 안전하게 수정하기 위해 복사본을 만드는 기능이었다.

이는 캐시에서 가져온 객체를 직접 수정할 때 발생할 수 있는 문제를 줄이기 위한 방식이었다.

초보자 입장에서는 DeepCopy를 Kubernetes 객체를 안전하게 수정하기 위한 복사 기능으로 이해했다.

---

## 28. 전체 흐름 최종 정리

Reconcile 함수의 전체 흐름은 아래와 같았다.

```text
1. 사용자가 Custom Resource를 생성하거나 수정했다.
        ↓
2. Controller가 이벤트를 감지했다.
        ↓
3. Reconcile 함수가 실행되었다.
        ↓
4. req.NamespacedName으로 대상 Custom Resource를 조회했다.
        ↓
5. Custom Resource가 삭제된 상태라면 정상 종료했다.
        ↓
6. Custom Resource가 존재하면 원하는 하위 리소스를 정의했다.
        ↓
7. OwnerReference를 설정해 부모-자식 관계를 만들었다.
        ↓
8. 현재 클러스터에 하위 리소스가 존재하는지 조회했다.
        ↓
9. 하위 리소스가 없으면 생성했다.
        ↓
10. 하위 리소스가 있으면 원하는 상태와 현재 상태를 비교했다.
        ↓
11. 상태가 다르면 업데이트했다.
        ↓
12. 필요한 경우 status를 업데이트했다.
        ↓
13. 정상 종료하거나 Requeue로 다시 실행을 예약했다.
```

---

## 29. 핵심 요약

`Reconcile` 함수는 사용자가 Custom Resource에 작성한 원하는 상태와 실제 Kubernetes 클러스터 상태를 계속 비교하고 맞추는 함수였다.

`desiredDeployment`는 원하는 상태였고, `existingDeployment`는 현재 실제 상태였다.

리소스가 없으면 `Create`를 수행했고, 리소스가 있으면 상태를 비교한 뒤 필요할 때만 `Update`를 수행했다.

`OwnerReference`는 Custom Resource와 하위 리소스 사이의 부모-자식 관계를 설정하는 기능이었다.

`Owns()`는 하위 리소스 변경 이벤트도 감지해 Reconcile을 다시 호출할 수 있게 했다.

`ctrl.Result{}`는 Reconcile 종료 후 다시 실행할지, 언제 다시 실행할지를 controller-runtime에게 알려주는 값이었다.

Kubernetes 내부 리소스는 이벤트 기반으로 다시 호출될 수 있었고, 외부 시스템은 Kubernetes가 상태 변화를 알 수 없기 때문에 `RequeueAfter`를 사용해 주기적으로 확인해야 했다.

따라서 Operator 개발에서 Reconcile은 단순한 생성 함수가 아니라, 원하는 상태를 지속적으로 유지하는 자동화 루프였다.
