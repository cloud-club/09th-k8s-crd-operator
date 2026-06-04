Finalizer 패턴과 Garbage Collection
1. Finalizer 패턴이란

Finalizer 패턴은 Kubernetes 리소스가 삭제되기 전에 Operator가 먼저 정리 작업을 수행하도록 삭제를 잠시 막아두는 패턴이다.

쉽게 말하면 다음과 같다.

Finalizer = 삭제 전에 반드시 실행해야 하는 정리 작업을 보장하는 장치

예를 들어 MyApp이라는 Custom Resource를 만들었을 때 Operator가 다음과 같은 리소스를 생성한다고 가정한다.

MyApp CR 생성
↓
Deployment 생성
Service 생성
Ingress 생성
외부 DB 생성
외부 S3 Bucket 생성
외부 DNS Record 생성

이때 사용자가 MyApp을 삭제하면 Kubernetes 내부 리소스는 어느 정도 자동으로 정리될 수 있다.

하지만 Kubernetes 외부에 생성된 리소스는 Kubernetes가 자동으로 알 수 없다.

예를 들어 다음과 같은 리소스가 있다.

AWS RDS
S3 Bucket
Route53 Record
외부 API 리소스
외부 SaaS 설정
클라우드 Load Balancer

이런 리소스들은 Operator가 직접 정리해야 한다.

Finalizer는 이러한 외부 리소스 정리 작업이 끝나기 전까지 Custom Resource가 완전히 삭제되지 않도록 막아주는 역할을 한다.

2. Finalizer가 필요한 이유

Finalizer가 없다면 삭제 흐름은 다음과 같이 진행될 수 있다.

사용자가 kubectl delete myapp sample-app 실행
↓
MyApp CR이 바로 삭제됨
↓
Operator가 삭제 전 정리 작업을 할 기회를 놓침
↓
외부 DB, S3, DNS 같은 리소스가 남음
↓
고아 리소스 발생
↓
비용 증가 / 보안 위험 / 운영 혼란 발생

즉, Finalizer는 삭제 시점의 안전장치이다.

특히 Operator가 Kubernetes 외부 리소스를 생성하거나 관리하는 경우 Finalizer는 매우 중요하다.

3. Finalizer가 있으면 삭제 흐름이 어떻게 바뀌는가

Finalizer가 붙어 있는 리소스를 삭제하면 Kubernetes는 해당 리소스를 바로 삭제하지 않는다.

대신 객체에 deletionTimestamp를 설정한다.

metadata:
  name: sample-app
  deletionTimestamp: "2026-06-03T10:00:00Z"
  finalizers:
    - apps.example.com/finalizer

이 상태는 다음과 같은 의미이다.

이 리소스는 삭제 요청을 받았다.
하지만 finalizer가 남아 있으므로 아직 완전히 삭제하지 않는다.
Controller가 정리 작업을 끝내고 finalizer를 제거해야 한다.

즉, Finalizer가 남아 있는 동안 리소스는 Terminating 상태에 머무를 수 있다.

4. Finalizer 패턴의 핵심 흐름

Finalizer 패턴은 Reconcile 함수 안에서 보통 다음과 같은 흐름으로 동작한다.

1. Custom Resource 조회
2. deletionTimestamp가 비어 있는지 확인
3. 삭제 중이 아니면 finalizer가 있는지 확인
4. finalizer가 없으면 추가
5. 일반 Reconcile 로직 수행
6. 삭제 요청이 들어오면 deletionTimestamp가 생김
7. finalizer가 있으면 외부 리소스 정리 작업 수행
8. 정리 성공 시 finalizer 제거
9. Kubernetes가 Custom Resource를 최종 삭제
5. 일반 상태와 삭제 상태 구분

Reconcile 함수에서는 보통 deletionTimestamp를 기준으로 일반 상태와 삭제 상태를 구분한다.

if myApp.ObjectMeta.DeletionTimestamp.IsZero() {
    // 일반 Reconcile 상태
} else {
    // 삭제 중인 상태
}

각 조건의 의미는 다음과 같다.

조건	의미
DeletionTimestamp.IsZero() == true	아직 삭제 요청이 들어오지 않은 일반 상태이다.
DeletionTimestamp.IsZero() == false	사용자가 삭제 요청을 했고, 현재 삭제 처리 중인 상태이다.
6. Finalizer 추가 흐름

삭제 중이 아닌 일반 상태에서는 finalizer가 없으면 추가한다.

const myAppFinalizer = "apps.example.com/finalizer"

if myApp.ObjectMeta.DeletionTimestamp.IsZero() {
    if !controllerutil.ContainsFinalizer(myApp, myAppFinalizer) {
        controllerutil.AddFinalizer(myApp, myAppFinalizer)

        if err := r.Update(ctx, myApp); err != nil {
            return ctrl.Result{}, err
        }
    }

    // 일반 Reconcile 로직 진행
}

여기서 중요한 점은 r.Status().Update()가 아니라 r.Update()를 사용한다는 것이다.

그 이유는 finalizer가 status가 아니라 metadata.finalizers에 들어가기 때문이다.

metadata:
  finalizers:
    - apps.example.com/finalizer

즉, Finalizer는 Status Subresource가 아니라 metadata 영역에 포함된다.

7. 삭제 요청이 들어왔을 때의 흐름

사용자가 Custom Resource를 삭제하면 Kubernetes는 객체를 바로 지우지 않고 deletionTimestamp를 설정한다.

그 다음 Reconcile이 다시 호출된다.

if !myApp.ObjectMeta.DeletionTimestamp.IsZero() {
    if controllerutil.ContainsFinalizer(myApp, myAppFinalizer) {
        // 1. 외부 리소스 정리
        if err := r.cleanupExternalResources(ctx, myApp); err != nil {
            return ctrl.Result{}, err
        }

        // 2. 정리 성공 후 finalizer 제거
        controllerutil.RemoveFinalizer(myApp, myAppFinalizer)

        if err := r.Update(ctx, myApp); err != nil {
            return ctrl.Result{}, err
        }
    }

    return ctrl.Result{}, nil
}

이 흐름의 의미는 다음과 같다.

삭제 요청이 들어왔다.
finalizer가 아직 남아 있다.
Operator가 외부 리소스를 먼저 정리한다.
정리 성공 후 finalizer를 제거한다.
finalizer가 사라지면 Kubernetes가 CR을 최종 삭제한다.
8. 전체 코드 흐름 예시
const myAppFinalizer = "apps.example.com/finalizer"

func (r *MyAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    myApp := &appv1.MyApp{}

    if err := r.Get(ctx, req.NamespacedName, myApp); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    if myApp.ObjectMeta.DeletionTimestamp.IsZero() {
        // 1. 삭제 중이 아니면 finalizer 등록
        if !controllerutil.ContainsFinalizer(myApp, myAppFinalizer) {
            controllerutil.AddFinalizer(myApp, myAppFinalizer)

            if err := r.Update(ctx, myApp); err != nil {
                return ctrl.Result{}, err
            }
        }

        // 2. 일반 Reconcile 로직
        // Deployment / Service / Ingress 생성 또는 수정
        // Status Conditions 업데이트
        return r.reconcileNormal(ctx, myApp)
    }

    // 3. 삭제 중인 경우
    if controllerutil.ContainsFinalizer(myApp, myAppFinalizer) {
        // 외부 리소스 정리
        if err := r.cleanupExternalResources(ctx, myApp); err != nil {
            return ctrl.Result{}, err
        }

        // 정리 성공 후 finalizer 제거
        controllerutil.RemoveFinalizer(myApp, myAppFinalizer)

        if err := r.Update(ctx, myApp); err != nil {
            return ctrl.Result{}, err
        }
    }

    return ctrl.Result{}, nil
}
9. Finalizer와 Status의 관계

Finalizer는 삭제를 잠시 막아두는 기능이고, Status는 삭제 진행 상태를 사용자에게 보여주는 역할을 할 수 있다.

예를 들어 삭제 요청이 들어오면 다음처럼 Status를 바꿀 수 있다.

status:
  conditions:
    - type: Deleting
      status: "True"
      reason: CleanupInProgress
      message: "External resources are being cleaned up."

정리 실패 시에는 다음처럼 기록할 수 있다.

status:
  conditions:
    - type: Deleting
      status: "True"
      reason: CleanupFailed
      message: "Failed to delete external database."

    - type: Degraded
      status: "True"
      reason: ExternalCleanupFailed
      message: "Manual intervention may be required."

또한 Event도 함께 남길 수 있다.

r.Recorder.Event(
    myApp,
    corev1.EventTypeWarning,
    "CleanupFailed",
    "Failed to clean up external resources",
)

즉, 삭제 시점에서도 Status 고도화 전략이 그대로 연결된다.

Finalizer = 삭제를 잠시 막고 정리 작업을 보장한다.
Status = 삭제가 어디까지 진행됐는지 보여준다.
Event = 삭제 과정에서 어떤 일이 있었는지 남긴다.
10. Garbage Collection이란

Garbage Collection은 Kubernetes가 OwnerReference를 기반으로 하위 리소스를 자동 삭제하는 기능이다.

예를 들어 MyApp이라는 Custom Resource가 있고, Operator가 Deployment와 Service를 만들었다고 하자.

MyApp
├── Deployment
└── Service

이때 Deployment와 Service에 OwnerReference가 설정되어 있으면, MyApp이 삭제될 때 Kubernetes Garbage Collector가 하위 리소스도 함께 정리한다.

Operator에서는 보통 다음과 같이 OwnerReference를 설정한다.

ctrl.SetControllerReference(myApp, deployment, r.Scheme)
ctrl.SetControllerReference(myApp, service, r.Scheme)

이렇게 설정하면 하위 리소스에는 다음과 같은 정보가 들어간다.

metadata:
  ownerReferences:
    - apiVersion: apps.example.com/v1
      kind: MyApp
      name: sample-app
      uid: ...

Kubernetes는 이 정보를 보고 다음과 같이 판단한다.

이 Deployment는 MyApp이 소유하고 있다.
MyApp이 삭제되면 Deployment도 같이 삭제해야 한다.
11. Finalizer와 Garbage Collection의 차이

Finalizer와 Garbage Collection은 둘 다 삭제와 관련이 있지만 역할은 다르다.

구분	Garbage Collection	Finalizer
목적	하위 리소스 자동 삭제	삭제 전 사용자 정의 정리 작업 수행
기준	ownerReferences	metadata.finalizers
주체	Kubernetes Garbage Collector	Operator / Controller
대상	Kubernetes 내부 리소스	외부 리소스 또는 특수 정리 작업
예시	Deployment, Service, ConfigMap 삭제	AWS RDS 삭제, S3 정리, DNS 제거, 백업 수행

정리하면 다음과 같다.

Garbage Collection은 Kubernetes가 알아서 지우는 것이다.
Finalizer는 Operator가 직접 지워야 하는 것을 처리하기 위한 것이다.
12. Finalizer와 Garbage Collection은 어떻게 같이 동작하는가

예를 들어 MyApp에 Finalizer가 있고, Deployment와 Service에는 OwnerReference가 있다고 하자.

metadata:
  name: sample-app
  finalizers:
    - apps.example.com/finalizer

삭제 요청이 들어오면 흐름은 다음과 같다.

1. 사용자가 kubectl delete myapp sample-app 실행
2. Kubernetes가 MyApp에 deletionTimestamp를 설정한다.
3. finalizer가 남아 있으므로 MyApp은 바로 삭제되지 않는다.
4. Operator가 Reconcile에서 deletionTimestamp를 감지한다.
5. Operator가 외부 리소스 정리 작업을 수행한다.
6. 정리 성공 후 finalizer를 제거한다.
7. MyApp이 최종 삭제된다.
8. OwnerReference가 걸린 Deployment/Service는 Garbage Collector가 정리한다.

즉, Finalizer가 있는 동안에는 부모 리소스가 완전히 삭제되지 않는다.

부모 리소스가 최종 삭제되면, 그 이후 Garbage Collection이 하위 리소스를 정리한다.

13. 실제 예시

MyApp이 다음과 같은 리소스를 관리한다고 하자.

MyApp CR
├── Deployment        Kubernetes 내부 리소스
├── Service           Kubernetes 내부 리소스
├── ConfigMap         Kubernetes 내부 리소스
├── AWS RDS           외부 리소스
└── Route53 Record    외부 리소스

이 경우 정리 전략은 다음과 같이 나눌 수 있다.

리소스	정리 방식
Deployment	OwnerReference + Garbage Collection
Service	OwnerReference + Garbage Collection
ConfigMap	OwnerReference + Garbage Collection
AWS RDS	Finalizer에서 직접 삭제
Route53 Record	Finalizer에서 직접 삭제

즉, Kubernetes 내부 리소스는 Garbage Collection에 맡기고, 외부 리소스는 Finalizer에서 정리하는 방식이 자연스럽다.

14. OwnerReference와 Finalizer를 같이 쓰는 이유

Operator가 하위 리소스를 만들 때는 보통 다음과 같이 OwnerReference를 설정한다.

ctrl.SetControllerReference(myApp, deployment, r.Scheme)

이건 Garbage Collection을 위한 설정이다.

그리고 Custom Resource 자체에는 Finalizer를 붙인다.

controllerutil.AddFinalizer(myApp, myAppFinalizer)

이건 삭제 전 정리 작업을 위한 설정이다.

두 개를 같이 쓰면 다음과 같은 구조가 된다.

OwnerReference
→ Kubernetes 내부 하위 리소스 자동 삭제

Finalizer
→ 외부 리소스 정리, 백업, 연결 해제 등 사용자 정의 삭제 로직 수행
15. Finalizer 사용 시 주의할 점

Finalizer는 강력하지만 잘못 사용하면 리소스가 삭제되지 않고 계속 Terminating 상태에 머물 수 있다.

대표적인 원인은 다음과 같다.

cleanupExternalResources 함수가 계속 실패함
Controller가 더 이상 실행되지 않음
finalizer 제거 로직이 없음
외부 API 권한이 부족함
삭제 대상 외부 리소스 이름을 잃어버림

이 경우 리소스는 다음 상태에 머물 수 있다.

Terminating

따라서 Finalizer를 사용할 때는 다음 원칙이 중요하다.

정리 작업은 가능하면 멱등적으로 만든다.
이미 삭제된 외부 리소스는 성공으로 처리한다.
실패 시 Event와 Status에 이유를 남긴다.
성공했을 때만 finalizer를 제거한다.
Controller가 죽으면 finalizer가 남아 삭제가 막힐 수 있음을 고려한다.
16. 멱등적인 cleanup이 중요한 이유

삭제 작업은 여러 번 재시도될 수 있다.

예를 들어 첫 번째 Reconcile에서 외부 DB를 삭제했는데, finalizer 제거 전에 네트워크 오류가 발생할 수 있다.

그러면 다음 Reconcile에서 다시 cleanup이 실행된다.

이때 외부 DB가 이미 삭제되어 있어도 에러로 보면 안 된다.

좋은 cleanup 로직은 다음과 같이 동작해야 한다.

외부 리소스가 존재하면 삭제한다.
외부 리소스가 이미 없으면 삭제 완료로 본다.
삭제가 아직 진행 중이면 Requeue한다.
권한 오류나 설정 오류는 Degraded 상태로 기록한다.

즉, cleanup 함수도 Reconcile처럼 멱등적으로 만들어야 한다.

17. 전체 삭제 흐름 정리

Finalizer와 Garbage Collection이 함께 사용되는 삭제 흐름은 다음과 같다.

사용자가 Custom Resource 삭제 요청
↓
Kubernetes가 deletionTimestamp 설정
↓
Finalizer가 남아 있으면 리소스를 바로 삭제하지 않음
↓
Operator가 deletionTimestamp 감지
↓
Operator가 외부 리소스 정리
↓
정리 성공 후 finalizer 제거
↓
Custom Resource 최종 삭제
↓
OwnerReference가 걸린 Kubernetes 내부 하위 리소스는 Garbage Collector가 정리