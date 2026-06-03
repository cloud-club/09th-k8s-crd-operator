# Operator 고도화

## 1. Status 고도화

Kubernetes에서 리소스는 크게 `spec`과 `status`로 나누어 관리된다.

Operator는 사용자가 정의한 `spec`을 기반으로 실제 Kubernetes 리소스를 생성하거나 수정하고, 그 결과를 `status`에 기록한다.

| 구분       | 의미          | 누가 수정하는가               |
| -------- | ----------- | ---------------------- |
| `spec`   | 사용자가 원하는 상태 | 사용자 / GitOps / kubectl |
| `status` | 현재 실제 상태    | Controller / Operator  |

즉, `spec`은 사용자가 원하는 목표 상태이고, `status`는 Operator가 실제 클러스터 상태를 관찰한 뒤 기록하는 현재 상태이다.

---

## 1-1. Kubernetes에서의 Status 고도화 전략

Status 고도화 전략은 단순히 `status.phase = Running`처럼 현재 상태를 문자열 하나로 기록하는 수준을 넘어서, 운영자가 현재 상태와 변화 원인을 더 정확하게 파악할 수 있도록 만드는 전략이다.

```text
Status 고도화 전략
├── 1. Conditions 패턴
│   └── Ready, Progressing, Degraded 등 복합 상태 표현
│
├── 2. ObservedGeneration 관리
│   └── status가 최신 spec 기준인지 확인
│
├── 3. Event + Status 연동
│   ├── status: 현재 상태
│   └── event: 상태 변화 원인 기록
│
├── 4. Progressing 중간 상태 세분화
│   └── Creating, RollingOut, WaitingForReplicas 등 표현
│
├── 5. LastTransitionTime 보존
│   └── 상태가 실제 전이될 때만 시간 갱신
│
└── 6. Status Subresource 분리
    └── spec과 status 업데이트 책임 분리
```

---

## 1-2. 각 전략은 Reconcile 함수 안에서 어떻게 이루어지는가

Status 고도화 전략은 Reconcile 함수와 밀접한 관련이 있다.

Reconcile 함수는 사용자가 원하는 상태인 `spec`과 실제 클러스터 상태를 비교하고, 부족한 리소스를 생성하거나 수정한 뒤, 그 결과를 `status`에 기록한다.

| 개념                    | Reconcile 안에서 하는 일                                                |
| --------------------- | ----------------------------------------------------------------- |
| Conditions 패턴         | Reconcile 결과를 `Ready`, `Progressing`, `Degraded` 같은 조건으로 기록한다.    |
| ObservedGeneration    | 현재 Reconcile이 어떤 `spec` 버전을 처리했는지 기록한다.                           |
| Event + Status        | 중요한 성공/실패 사건은 Event로 남기고, 최종 상태는 Status로 남긴다.                     |
| Progressing 패턴        | 아직 완료되지 않은 중간 상태를 세분화해서 표현한다.                                     |
| LastTransitionTime 보존 | Condition의 `status`가 실제로 바뀔 때만 시간을 갱신한다.                          |
| Status Subresource    | `r.Status().Update()` 또는 `r.Status().Patch()`로 status만 따로 업데이트한다. |

즉, Reconcile 함수는 실제 작업을 수행하는 공간이고, Status는 그 작업 결과를 사용자와 운영자에게 보여주는 기록 공간이다.

---

# 2. Conditions 패턴

## 2-1. Conditions 패턴이란

Conditions 패턴은 `status.conditions` 아래에 여러 개의 상태 조건을 배열 형태로 저장하는 패턴이다.

단순히 현재 상태를 보여주는 뷰어 역할이 아니라, Kubernetes Operator의 상태 업데이트 전략과 Reconciliation Loop를 고도화하는 핵심 기반이 된다.

기본 구조는 다음과 같다.

```yaml
status:
  conditions:
    - type: Ready
      status: "False"
      reason: DiskPressure
      message: "The node is running out of disk space."
      lastTransitionTime: "2026-06-03T08:00:00Z"
```

각 필드의 의미는 다음과 같다.

| 필드                   | 의미                                                                  |
| -------------------- | ------------------------------------------------------------------- |
| `type`               | 검사 대상이 되는 조건의 종류이다. 예를 들어 `Ready`, `Progressing`, `Degraded` 등이 있다. |
| `status`             | 해당 조건이 만족되었는지를 나타낸다. 값은 `"True"`, `"False"`, `"Unknown"` 중 하나이다.    |
| `reason`             | 상태의 원인을 CamelCase 형태로 나타낸다.                                         |
| `message`            | 사람이 읽고 디버깅할 수 있도록 상세한 설명을 담는다.                                      |
| `lastTransitionTime` | Condition의 `status` 값이 마지막으로 변경된 시각이다.                              |

---

## 2-2. Conditions 패턴이 Status 업데이트를 고도화하는 이유

### 1) 멱등성 유지가 쉬워진다

Operator는 Reconcile이 반복 실행되어도 같은 결과를 안정적으로 만들어야 한다.

예를 들어 데이터베이스 마이그레이션이 이미 끝난 상태라면, Operator는 매번 마이그레이션을 다시 수행하면 안 된다.

이때 Conditions를 체크포인트처럼 사용할 수 있다.

```yaml
status:
  conditions:
    - type: DatabaseMigrated
      status: "True"
      reason: MigrationCompleted
      message: "Database migration completed successfully."
```

Reconcile 함수는 이 Condition을 보고 다음과 같이 판단할 수 있다.

```text
DatabaseMigrated=True 상태가 이미 존재한다.
따라서 데이터베이스 마이그레이션 단계는 건너뛰고 다음 단계로 진행한다.
```

즉, Conditions는 복잡한 다단계 배포 과정에서 체크포인트 역할을 한다.

---

### 2) 세밀한 Backoff 및 에러 핸들링 전략을 세울 수 있다

Operator가 작업을 수행하다가 실패했을 때, Conditions의 `reason`과 `lastTransitionTime`은 에러 복구 전략의 기반이 된다.

예를 들어 다음과 같은 상태가 있다고 하자.

```yaml
status:
  conditions:
    - type: APIReady
      status: "False"
      reason: ConnectionRefused
      message: "External API connection was refused."
      lastTransitionTime: "2026-06-03T10:00:00Z"
```

만약 이 상태가 5분 이상 지속되고 있다면, Operator는 단순 네트워크 지연이 아니라 지속적인 장애로 판단할 수 있다.

이 경우 다음과 같은 고도화된 처리가 가능하다.

```text
관리자에게 Alert을 보낸다.
Requeue 간격을 늘린다.
특정 횟수 이상 실패하면 Degraded=True로 전환한다.
필요하면 롤백 로직을 실행한다.
```

또한 `reason`을 통해 사용자 잘못인지, 일시적 서버 오류인지 구분할 수 있다.

예를 들어 다음과 같이 나눌 수 있다.

| reason              | 의미                   | 처리 전략                      |
| ------------------- | -------------------- | -------------------------- |
| `InvalidSpec`       | 사용자가 잘못된 spec을 입력했다. | 즉시 Degraded 처리하고 사용자 수정 대기 |
| `ConnectionTimeout` | 외부 시스템 응답이 늦다.       | 일정 시간 후 재시도                |
| `ImagePullBackOff`  | 이미지 Pull에 실패했다.      | Event 기록 및 Degraded 처리     |

---

### 3) 다중 비동기 작업 상태를 추적할 수 있다

복잡한 Operator는 CR 하나를 만들었을 때 내부적으로 여러 하위 작업을 동시에 처리할 수 있다.

예를 들어 다음과 같은 작업이 있을 수 있다.

```text
데이터베이스 생성
스토리지 할당
설정 파일 생성
Deployment 생성
Service 생성
Ingress 생성
```

이때 Conditions를 사용하면 여러 하위 시스템의 상태를 독립적으로 추적할 수 있다.

```yaml
status:
  conditions:
    - type: DatabaseReady
      status: "True"
      reason: DatabaseProvisioned
      message: "Database is ready."

    - type: StorageReady
      status: "True"
      reason: StorageBound
      message: "Persistent volume is bound."

    - type: ApplicationReady
      status: "False"
      reason: WaitingForReplicas
      message: "1 of 3 replicas are ready."
```

이 구조를 사용하면 전체 애플리케이션이 아직 Ready가 아니더라도, 어떤 단계까지 완료되었는지 명확하게 확인할 수 있다.

---

# 3. ObservedGeneration

## 3-1. ObservedGeneration이란

`ObservedGeneration`은 특정 `status`가 몇 번째 `spec` 변경을 기준으로 계산된 상태인지를 나타내는 값이다.

Kubernetes 리소스에는 `metadata.generation`이라는 값이 있다.

이 값은 사용자가 `spec`을 수정할 때마다 증가한다.

```yaml
metadata:
  generation: 2
```

그리고 Operator가 해당 generation을 관찰하고 처리한 뒤, 그 결과를 status에 기록하면 다음과 같이 표시할 수 있다.

```yaml
status:
  observedGeneration: 2
```

이 뜻은 다음과 같다.

```text
Operator가 generation 2의 spec을 보고 처리했고, 그 결과를 status에 기록했다.
```

---

## 3-2. generation과 observedGeneration 비교

정상적인 상태는 다음과 같다.

```yaml
metadata:
  generation: 2

status:
  observedGeneration: 2
```

이 경우 현재 status는 최신 spec을 기준으로 계산된 상태이다.

반대로 다음과 같은 상태는 주의해야 한다.

```yaml
metadata:
  generation: 3

status:
  observedGeneration: 2
```

이 경우 사용자가 spec을 generation 3으로 수정했지만, Operator가 아직 그 변경사항을 status에 반영하지 못한 상태이다.

즉, 현재 status는 이전 spec 기준의 결과일 수 있다.

---

## 3-3. 왜 generation과 observedGeneration 비교가 중요한가

Operator는 비동기로 동작한다.

사용자가 CR을 수정했다고 해서 Operator가 즉시 처리하는 것은 아니다.

흐름은 다음과 같다.

```text
사용자가 spec 수정
↓
metadata.generation 증가
↓
Reconcile 이벤트 발생
↓
Operator가 변경사항 처리
↓
하위 리소스 생성/수정
↓
status 업데이트
↓
observedGeneration 갱신
```

따라서 운영자는 단순히 `Ready=True`만 보면 안 된다.

다음 두 조건을 같이 확인해야 한다.

```text
Ready=True 인가?
Ready=True가 최신 generation 기준인가?
```

---

## 3-4. ObservedGeneration은 어디에 둘 수 있는가

ObservedGeneration은 보통 두 위치에 둘 수 있다.

### 1) status 전체 레벨

```yaml
status:
  observedGeneration: 5
```

이는 전체 status가 generation 5를 기준으로 계산되었다는 뜻이다.

### 2) condition 레벨

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: DeploymentAvailable
      observedGeneration: 5
```

이는 해당 `Ready` Condition이 generation 5를 기준으로 계산되었다는 뜻이다.

실무적으로는 둘 다 사용할 수 있다.

```yaml
status:
  observedGeneration: 5
  conditions:
    - type: Ready
      status: "True"
      observedGeneration: 5
    - type: Progressing
      status: "False"
      observedGeneration: 5
```

---

## 3-5. 언제 observedGeneration을 갱신하는가

ObservedGeneration은 Operator가 해당 spec generation을 실제로 관찰하고, 그 기준으로 status를 계산했을 때 갱신한다.

기준은 다음과 같다.

```text
처리 결과를 status에 명확히 반영하면서 observedGeneration을 갱신한다.
실패하더라도 실패 condition과 함께 observedGeneration을 갱신한다.
아예 처리하지 못한 경우에는 갱신하지 않는다.
```

예를 들어 실패했더라도 아래처럼 실패 condition을 명확히 남긴다면 observedGeneration을 갱신할 수 있다.

```yaml
status:
  observedGeneration: 6
  conditions:
    - type: Ready
      status: "False"
      reason: DeploymentCreateFailed
      message: "Failed to create Deployment."
      observedGeneration: 6
```

이 상태는 다음과 같이 해석할 수 있다.

```text
Operator가 generation 6의 spec을 처리하려고 했고, 그 결과 Deployment 생성에 실패했다.
```

---

## 3-6. Reconcile 함수 안에서의 판단 로직

Status가 최신 spec 기준인지 확인하는 함수는 다음과 같이 작성할 수 있다.

```go
func isStatusFresh(obj *appv1.MyApp) bool {
    return obj.Status.ObservedGeneration == obj.Generation
}
```

Ready 상태를 판단할 때는 `Ready=True`만 확인하면 부족하다.

`observedGeneration`까지 함께 확인해야 한다.

```go
func isReady(obj *appv1.MyApp) bool {
    if obj.Status.ObservedGeneration != obj.Generation {
        return false
    }

    for _, condition := range obj.Status.Conditions {
        if condition.Type == "Ready" &&
            condition.Status == metav1.ConditionTrue &&
            condition.ObservedGeneration == obj.Generation {
            return true
        }
    }

    return false
}
```

즉, 진짜 Ready 판단 기준은 다음과 같다.

```text
Ready=True인가?
그리고 그 Ready condition이 최신 generation 기준인가?
```

---

# 4. Event + Status 연동

## 4-1. Event와 Status의 역할

Event와 Status는 모두 Operator 상태 추적에 사용되지만 역할이 다르다.

| 구분       | 역할                 | 예시                                       |
| -------- | ------------------ | ---------------------------------------- |
| `Status` | 현재 최종 상태           | 현재 Ready인지, Progressing인지, Degraded인지    |
| `Event`  | 상태가 바뀐 원인 또는 사건 기록 | Deployment 생성 성공, Pod 생성 실패, 이미지 Pull 실패 |

쉽게 말하면 다음과 같다.

```text
Status = 지금 상태
Event = 왜 그렇게 됐는지에 대한 사건 로그
```

---

## 4-2. Status만으로 부족한 이유

Status를 보면 현재 상태를 알 수 있다.

```yaml
status:
  conditions:
    - type: Degraded
      status: "True"
      reason: DeploymentCreateFailed
      message: "Failed to create Deployment"
```

하지만 이것만으로는 다음 정보를 확인하기 어렵다.

```text
언제 실패했는가?
몇 번 실패했는가?
중간에 어떤 일이 있었는가?
같은 원인으로 반복 실패했는가?
복구 이벤트가 있었는가?
```

이런 정보는 Event를 통해 파악하는 것이 적합하다.

```text
Events:
  Type     Reason                    Message
  ----     ------                    -------
  Normal   DeploymentCreating         Deployment is being created
  Warning  DeploymentCreateFailed     Failed to create Deployment
  Normal   ReconcileRetry             Retrying reconciliation
```

즉, 사용자는 Status로 현재 상태를 확인하고, Event로 그 과정과 원인을 확인할 수 있다.

---

## 4-3. Event를 남기는 기준

Event를 무작정 많이 생성하는 것은 좋은 전략이 아니다.

Reconcile은 반복적으로 실행되기 때문에, 매번 Event를 남기면 Event가 지나치게 많이 쌓이고 오히려 디버깅이 어려워질 수 있다.

따라서 Event는 중요한 상태 변화 또는 실패 상황에서만 남기는 것이 좋다.

예시는 다음과 같다.

```text
Pending → Progressing
Progressing → Ready
Progressing → Degraded
Degraded → Ready
삭제 시작
외부 시스템 호출 실패
리소스 생성 실패
복구 성공
```

---

## 4-4. Reconcile 함수 안에서의 Event + Status 연동

Deployment 생성에 실패한 경우는 다음과 같이 처리할 수 있다.

```go
if err := r.Create(ctx, desiredDeploy); err != nil {
    myApp.Status.ObservedGeneration = myApp.Generation

    setCondition(&myApp.Status.Conditions, metav1.Condition{
        Type:               "Degraded",
        Status:             metav1.ConditionTrue,
        Reason:             "DeploymentCreateFailed",
        Message:            "Failed to create Deployment",
        ObservedGeneration: myApp.Generation,
    })

    r.Recorder.Event(
        myApp,
        corev1.EventTypeWarning,
        "DeploymentCreateFailed",
        "Failed to create Deployment",
    )

    return r.updateStatusIfChanged(ctx, original, myApp)
}
```

여기서 핵심은 다음과 같다.

```text
Status에는 현재 상태를 Degraded=True로 기록했다.
Event에는 DeploymentCreateFailed라는 사건을 Warning으로 남겼다.
```

---

## 4-5. Event + Status 연동의 효과

Event와 Status를 함께 사용하면 다음과 같은 장점이 있다.

```text
현재 상태를 빠르게 파악할 수 있다.
상태가 바뀐 원인을 추적할 수 있다.
장애가 반복되는지 확인할 수 있다.
운영자가 kubectl describe로 상태 변화 흐름을 확인할 수 있다.
Status 업데이트의 의미와 디버깅 가능성이 높아진다.
```

---

# 5. Progressing 패턴

## 5-1. Progressing 패턴이 필요한 이유

Operator는 CR을 생성하거나 수정할 때 바로 성공하거나 실패하지 않는다.

일반적으로 여러 중간 단계를 거친다.

```text
CR 생성
Deployment 생성
ReplicaSet 생성
Pod 스케줄링
이미지 Pull
Container 시작
Readiness Probe 통과
Service 연결
Ingress 준비
최종 Ready
```

이 과정에서 단순히 `Ready=False`만 기록하면 사용자는 현재 상태를 정확히 이해하기 어렵다.

```text
아직 정상적으로 진행 중인 것인가?
실패한 것인가?
기다리면 되는 것인가?
사용자가 수정해야 하는 것인가?
```

이를 구분하기 위해 `Progressing` Condition을 사용한다.

---

## 5-2. Progressing의 의미

`Progressing=True`는 다음과 같은 의미이다.

```text
아직 Ready는 아니지만, 원하는 상태로 정상적으로 진행 중이다.
```

예시는 다음과 같다.

```yaml
status:
  conditions:
    - type: Ready
      status: "False"
      reason: WaitingForReplicas
      message: "1 of 3 replicas are ready"

    - type: Progressing
      status: "True"
      reason: WaitingForReplicas
      message: "Deployment rollout is in progress"
```

이 상태는 실패가 아니다.

아직 모든 Pod가 준비되지는 않았지만, 정상적으로 배포가 진행 중인 상태이다.

---

## 5-3. Condition Type은 적게, 세부 상태는 Reason으로 표현한다

Progressing 패턴에서 중요한 점은 Condition Type을 지나치게 많이 만들지 않는 것이다.

비추천 예시는 다음과 같다.

```yaml
conditions:
  - type: DeploymentCreating
    status: "True"
  - type: WaitingForReplicas
    status: "True"
  - type: ImagePulling
    status: "True"
```

이렇게 Condition Type을 너무 많이 만들면 상태 구조가 복잡해지고 관리하기 어려워진다.

추천하는 방식은 다음과 같다.

```yaml
conditions:
  - type: Progressing
    status: "True"
    reason: DeploymentCreating
    message: "Deployment is being created"
```

또는 다음과 같이 표현한다.

```yaml
conditions:
  - type: Progressing
    status: "True"
    reason: WaitingForReplicas
    message: "1 of 3 replicas are ready"
```

즉, 큰 상태는 `Progressing`으로 두고, 세부 원인은 `reason`과 `message`로 표현하는 것이 좋다.

---

## 5-4. Progressing 과정 세분화 예시

### 1) Deployment가 아직 없음

```yaml
conditions:
  - type: Ready
    status: "False"
    reason: DeploymentNotFound
    message: "Deployment has not been created yet"

  - type: Progressing
    status: "True"
    reason: DeploymentCreating
    message: "Deployment is being created"
```

### 2) Deployment는 있지만 replica 부족

```yaml
conditions:
  - type: Ready
    status: "False"
    reason: WaitingForReplicas
    message: "1 of 3 replicas are ready"

  - type: Progressing
    status: "True"
    reason: WaitingForReplicas
    message: "Waiting for all replicas to become ready"
```

### 3) 이미지 Pull 실패

이미지 Pull 실패는 단순 진행 중 상태가 아니라 장애 상태에 가깝다.

```yaml
conditions:
  - type: Ready
    status: "False"
    reason: ImagePullBackOff
    message: "Pod cannot pull image"

  - type: Progressing
    status: "False"
    reason: RolloutBlocked
    message: "Rollout is blocked by image pull error"

  - type: Degraded
    status: "True"
    reason: ImagePullBackOff
    message: "Image pull failed"
```

---

## 5-5. Reconcile 함수 안에서의 Progressing 처리

사용자가 원하는 replica 수보다 실제 준비된 Pod 개수가 적다면, Reconcile 함수는 다음과 같이 `Ready=False`, `Progressing=True`를 기록할 수 있다.

```go
if deploy.Status.ReadyReplicas < myApp.Spec.Replicas {
    setCondition(&myApp.Status.Conditions, metav1.Condition{
        Type:               "Ready",
        Status:             metav1.ConditionFalse,
        Reason:             "WaitingForReplicas",
        Message:            fmt.Sprintf("%d of %d replicas are ready", deploy.Status.ReadyReplicas, myApp.Spec.Replicas),
        ObservedGeneration: myApp.Generation,
    })

    setCondition(&myApp.Status.Conditions, metav1.Condition{
        Type:               "Progressing",
        Status:             metav1.ConditionTrue,
        Reason:             "WaitingForReplicas",
        Message:            "Deployment rollout is in progress",
        ObservedGeneration: myApp.Generation,
    })
}
```

이 코드는 다음을 의미한다.

```text
아직 Ready는 아니다.
하지만 배포가 정상적으로 진행 중이다.
이유는 원하는 replica 수보다 readyReplicas 수가 적기 때문이다.
```

---

# 6. LastTransitionTime 보존

## 6-1. LastTransitionTime이란

`LastTransitionTime`은 Condition의 `status`가 마지막으로 바뀐 시간이다.

예를 들어 다음과 같은 상태가 있다고 하자.

```yaml
conditions:
  - type: Ready
    status: "False"
    reason: WaitingForReplicas
    lastTransitionTime: "2026-06-03T10:00:00Z"
```

이 뜻은 다음과 같다.

```text
Ready Condition이 False 상태로 바뀐 시간이 2026-06-03 10:00:00이다.
```

즉, `lastTransitionTime`은 Reconcile 함수가 실행된 시간이 아니라, Condition의 상태가 실제로 전이된 시간이다.

---

## 6-2. 잘못된 LastTransitionTime 관리

다음처럼 Reconcile이 실행될 때마다 `lastTransitionTime`이 바뀌면 안 된다.

```yaml
lastTransitionTime: "2026-06-03T10:00:00Z"
lastTransitionTime: "2026-06-03T10:01:00Z"
lastTransitionTime: "2026-06-03T10:02:00Z"
lastTransitionTime: "2026-06-03T10:03:00Z"
```

이 방식은 보존이 아니라 매 Reconcile마다 시간을 덮어쓰는 것이다.

이렇게 되면 운영자는 다음을 알 수 없게 된다.

```text
Ready=False 상태가 실제로 언제부터 지속되었는가?
장애가 1분 전부터 있었는가?
아니면 1시간 전부터 있었는가?
```

또한 status가 매번 변경되기 때문에 불필요한 Status Update와 Reconcile 재호출이 발생할 수 있다.

---

## 6-3. 올바른 LastTransitionTime 보존 방식

예를 들어 10:00에 처음 `Ready=False`가 되었다고 하자.

```yaml
conditions:
  - type: Ready
    status: "False"
    reason: WaitingForReplicas
    lastTransitionTime: "2026-06-03T10:00:00Z"
```

그 이후 10:01, 10:02, 10:03에도 계속 `Ready=False`라면 `lastTransitionTime`은 그대로 유지되어야 한다.

```yaml
conditions:
  - type: Ready
    status: "False"
    reason: WaitingForReplicas
    lastTransitionTime: "2026-06-03T10:00:00Z"
```

즉, 상태가 바뀌지 않았다면 시간이 바뀌면 안 된다.

반대로 10:05에 `Ready=False`에서 `Ready=True`로 바뀌었다면 그때는 갱신해야 한다.

```yaml
conditions:
  - type: Ready
    status: "True"
    reason: DeploymentAvailable
    lastTransitionTime: "2026-06-03T10:05:00Z"
```

---

## 6-4. LastTransitionTime 갱신 기준

기준은 다음과 같다.

```text
Ready=False → Ready=False
= lastTransitionTime 유지

Ready=False → Ready=True
= lastTransitionTime 갱신

Progressing=True → Progressing=True
= lastTransitionTime 유지

Progressing=True → Progressing=False
= lastTransitionTime 갱신

Degraded=False → Degraded=True
= lastTransitionTime 갱신
```

즉, `reason`이나 `message`만 바뀌었다고 무조건 갱신하는 것이 아니다.

보통은 Condition의 `status` 값이 `"True"`, `"False"`, `"Unknown"` 사이에서 실제로 바뀔 때 갱신한다.

---

## 6-5. Reconcile 함수에서의 좋은 LastTransitionTime 보존 설계

`LastTransitionTime`을 보존하려면 Condition을 단순히 덮어쓰면 안 된다.

기존 Condition과 새로운 Condition을 비교한 뒤, `status` 값이 바뀐 경우에만 시간을 새로 넣어야 한다.

```go
func setCondition(conditions *[]metav1.Condition, newCondition metav1.Condition) {
    now := metav1.Now()

    for i, existing := range *conditions {
        if existing.Type == newCondition.Type {
            if existing.Status != newCondition.Status {
                newCondition.LastTransitionTime = now
            } else {
                newCondition.LastTransitionTime = existing.LastTransitionTime
            }

            (*conditions)[i] = newCondition
            return
        }
    }

    newCondition.LastTransitionTime = now
    *conditions = append(*conditions, newCondition)
}
```

이 함수의 핵심은 다음과 같다.

```text
상태가 바뀌었으면 새 시간을 기록한다.
상태가 그대로면 기존 시간을 유지한다.
처음 생성되는 Condition이면 현재 시간을 기록한다.
```

---

# 7. Status Subresource 분리

## 7-1. Status Subresource란

Custom Resource는 보통 `spec`과 `status`를 나누어 관리한다.

```yaml
spec:
  replicas: 3
  image: nginx:1.25

status:
  readyReplicas: 2
  conditions:
    - type: Ready
      status: "False"
```

두 영역의 역할은 명확히 다르다.

| 구분       | 의미          | 관리 주체                |
| -------- | ----------- | -------------------- |
| `spec`   | 사용자가 원하는 상태 | 사용자, GitOps, kubectl |
| `status` | 현재 실제 상태    | Controller, Operator |

`Status Subresource`는 이 둘을 Kubernetes API 레벨에서 분리하는 기능이다.

즉, Custom Resource 전체를 업데이트하는 것이 아니라, `/status` 경로를 통해 status만 따로 업데이트할 수 있게 만든다.

---

## 7-2. Status Subresource가 필요한 이유

Status Subresource가 필요한 이유는 `spec`과 `status`의 책임을 분리하기 위해서이다.

Kubernetes Operator의 기본 책임 구조는 다음과 같다.

```text
사용자 또는 GitOps 도구는 spec을 수정한다.
Operator 또는 Controller는 status를 수정한다.
```

만약 Operator가 일반 `Update()`로 전체 객체를 수정하면, 의도치 않게 `spec`까지 건드릴 위험이 있다.

```go
r.Update(ctx, myApp)
```

이 방식은 객체 전체를 업데이트하는 방식이다.

따라서 Status만 업데이트하려는 상황에서는 적절하지 않다.

Status Subresource를 활성화하면 Operator는 다음처럼 status만 따로 수정할 수 있다.

```go
r.Status().Update(ctx, myApp)
```

또는 다음처럼 Patch 방식으로 수정할 수도 있다.

```go
r.Status().Patch(ctx, myApp, client.MergeFrom(original))
```

이렇게 하면 `spec`은 건드리지 않고 `status`만 업데이트할 수 있다.

---

## 7-3. Kubebuilder에서 Status Subresource 활성화

Kubebuilder에서는 Custom Resource 타입 위에 다음 마커를 추가한다.

```go
// +kubebuilder:subresource:status
type MyApp struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   MyAppSpec   `json:"spec,omitempty"`
    Status MyAppStatus `json:"status,omitempty"`
}
```

이 마커를 추가하면 CRD에는 다음과 같은 설정이 생성된다.

```yaml
subresources:
  status: {}
```

이 설정이 있어야 `/status` subresource를 통해 status 업데이트를 분리할 수 있다.

---

## 7-4. Reconcile 함수에서 Status Subresource 사용 방식

Reconcile 함수에서는 보통 다음 순서로 status를 업데이트한다.

```text
1. Custom Resource 조회
2. 기존 객체를 DeepCopy로 저장
3. 실제 클러스터 상태 확인
4. status 값 계산
5. 기존 status와 새 status 비교
6. 변경된 경우에만 r.Status().Update() 또는 r.Status().Patch() 실행
```

예시는 다음과 같다.

```go
func (r *MyAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    myApp := &appv1.MyApp{}

    if err := r.Get(ctx, req.NamespacedName, myApp); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    original := myApp.DeepCopy()

    deploy := &appsv1.Deployment{}
    err := r.Get(ctx, types.NamespacedName{
        Name:      myApp.Name,
        Namespace: myApp.Namespace,
    }, deploy)

    if err != nil {
        myApp.Status.ObservedGeneration = myApp.Generation

        setCondition(&myApp.Status.Conditions, metav1.Condition{
            Type:               "Ready",
            Status:             metav1.ConditionFalse,
            Reason:             "DeploymentNotFound",
            Message:            "Deployment has not been created yet",
            ObservedGeneration: myApp.Generation,
        })

        return r.updateStatusIfChanged(ctx, original, myApp)
    }

    myApp.Status.ObservedGeneration = myApp.Generation
    myApp.Status.ReadyReplicas = deploy.Status.ReadyReplicas

    if deploy.Status.ReadyReplicas == myApp.Spec.Replicas {
        setCondition(&myApp.Status.Conditions, metav1.Condition{
            Type:               "Ready",
            Status:             metav1.ConditionTrue,
            Reason:             "DeploymentAvailable",
            Message:            "All replicas are ready",
            ObservedGeneration: myApp.Generation,
        })
    }

    return r.updateStatusIfChanged(ctx, original, myApp)
}
```

Status 업데이트 보조 함수는 다음과 같이 만들 수 있다.

```go
func (r *MyAppReconciler) updateStatusIfChanged(
    ctx context.Context,
    original *appv1.MyApp,
    current *appv1.MyApp,
) (ctrl.Result, error) {
    if reflect.DeepEqual(original.Status, current.Status) {
        return ctrl.Result{}, nil
    }

    patch := client.MergeFrom(original)

    if err := r.Status().Patch(ctx, current, patch); err != nil {
        return ctrl.Result{}, err
    }

    return ctrl.Result{}, nil
}
```

이 구조의 핵심은 다음과 같다.

```text
status가 실제로 변경된 경우에만 업데이트한다.
업데이트는 status subresource를 통해 수행한다.
spec은 건드리지 않는다.
```

---

## 7-5. Status().Update()와 Status().Patch()의 차이

Status Subresource를 사용할 때는 보통 `Update()` 또는 `Patch()`를 사용한다.

| 방식                                  | 특징                                |
| ----------------------------------- | --------------------------------- |
| `r.Status().Update(ctx, obj)`       | status 전체를 업데이트한다. 구현이 단순하다.      |
| `r.Status().Patch(ctx, obj, patch)` | 변경된 부분 중심으로 반영한다. 충돌 가능성을 줄이기 좋다. |

간단한 학습용 Operator라면 `Status().Update()`만으로도 충분하다.

```go
if err := r.Status().Update(ctx, myApp); err != nil {
    return ctrl.Result{}, err
}
```

하지만 실무적으로는 `Patch()`를 선호하는 경우가 많다.

```go
patch := client.MergeFrom(original)

if err := r.Status().Patch(ctx, myApp, patch); err != nil {
    return ctrl.Result{}, err
}
```

`Patch()`는 기존 객체를 기준으로 변경된 부분만 반영하기 때문에, 불필요한 충돌을 줄이는 데 유리하다.

---

## 7-6. Status Subresource와 RBAC

Status Subresource를 사용하면 RBAC 권한도 분리할 수 있다.

예를 들어 일반 리소스와 status 리소스에 대한 권한을 따로 줄 수 있다.

```yaml
# 일반 Custom Resource 권한
- apiGroups:
    - apps.example.com
  resources:
    - myapps
  verbs:
    - get
    - list
    - watch
    - create
    - update
    - patch
    - delete

# status subresource 권한
- apiGroups:
    - apps.example.com
  resources:
    - myapps/status
  verbs:
    - get
    - update
    - patch
```

이렇게 하면 Operator는 `myapps/status`에 대한 업데이트 권한을 가지고 status만 수정할 수 있다.

즉, 사용자와 Operator의 책임을 권한 레벨에서도 분리할 수 있다.

---

## 7-7. Status Subresource 사용 시 주의할 점

Status 업데이트도 Kubernetes 리소스 변경 이벤트에 해당한다.

따라서 status를 업데이트하면 Reconcile이 다시 호출될 수 있다.

그래서 매 Reconcile마다 status를 무조건 업데이트하면 안 된다.

비추천 예시는 다음과 같다.

```go
myApp.Status.ReadyReplicas = deploy.Status.ReadyReplicas
r.Status().Update(ctx, myApp)
```

이 방식은 status가 실제로 바뀌지 않았는데도 매번 업데이트할 수 있다.

그 결과 다음과 같은 문제가 생길 수 있다.

```text
불필요한 API Server 요청 증가
불필요한 Reconcile 반복
resourceVersion 충돌 가능성 증가
디버깅 어려움 증가
```

따라서 좋은 방식은 다음과 같다.

```go
original := myApp.DeepCopy()

myApp.Status.ReadyReplicas = deploy.Status.ReadyReplicas

if !reflect.DeepEqual(original.Status, myApp.Status) {
    if err := r.Status().Patch(ctx, myApp, client.MergeFrom(original)); err != nil {
        return ctrl.Result{}, err
    }
}
```

즉, status가 실제로 바뀐 경우에만 `/status` subresource를 통해 업데이트해야 한다.

---

## 7-8. Status Subresource 분리의 장점

Status Subresource를 사용하면 다음과 같은 장점이 있다.

| 장점                   | 설명                                                       |
| -------------------- | -------------------------------------------------------- |
| spec/status 책임 분리    | 사용자는 spec을 수정하고, Operator는 status를 수정하는 구조를 명확히 만들 수 있다. |
| 실수 방지                | status 업데이트 중 spec을 잘못 수정하는 문제를 줄일 수 있다.                 |
| RBAC 분리              | status 업데이트 권한만 별도로 부여할 수 있다.                            |
| Kubernetes API 관례 준수 | Kubernetes 내장 리소스와 비슷한 방식으로 상태를 관리할 수 있다.                |
| 운영 안정성 향상            | 불필요한 전체 객체 업데이트를 줄이고, status 중심의 안전한 업데이트가 가능하다.         |

---

# 8. 최종 정리

Kubernetes Operator에서 Status 고도화는 Reconcile 함수의 결과를 얼마나 정확하고 운영 친화적으로 표현하느냐의 문제이다.

```text
Reconcile 함수는 실제 리소스를 맞추는 작업을 수행한다.
Status는 그 작업 결과를 현재 상태로 기록한다.
Event는 그 과정에서 발생한 중요한 사건을 기록한다.
Conditions는 상태를 구조적으로 표현한다.
ObservedGeneration은 status가 최신 spec 기준인지 확인한다.
Progressing은 중간 진행 상태를 표현한다.
LastTransitionTime은 상태가 실제로 바뀐 시간을 보존한다.
Status Subresource는 spec과 status의 책임을 API 레벨에서 분리한다.
```