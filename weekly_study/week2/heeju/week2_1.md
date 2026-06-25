# K8s Controller 패턴

> - Kubernetes가 어떻게 원하는 상태를 지속적으로 유지하는지 이해하고, 이를 가능하게 하는 Controller 패턴의 동작 원리를 학습합니다.
> - 또한 Kubernetes의 선언형(Declarative) 철학과 Reconcile 메커니즘이 어떤 방식으로 상태를 관리하는지 이해하는 것을 목표로 합니다.

## 목차

1. Kubernetes는 어떻게 상태를 유지하는가
2. 명령형과 선언형 설계 철학
3. Desired State와 Actual State
4. Controller 패턴
5. Reconcile의 본질
6. Self-Healing과 상태 수렴

## 1. Kubernetes는 어떻게 상태를 유지하는가

Kubernetes는 단순히 컨테이너를 실행하는 플랫폼이 아니다. 핵심 목표는 **사용자가 원하는 상태를 지속적으로 유지하는 것**이다.

예를 들어 사용자가 다음과 같이 선언했다고 가정해보자.
`spec: replicas: 3`

이 의미는 지금 Pod 3개를 만들어라가 아니라, **항상 Pod 3개 상태를 유지해라**에 가깝다. 만약 실행 중인 Pod 하나가 삭제되더라도 Kubernetes는 자동으로 새로운 Pod를 생성한다.

즉 Kubernetes는 한 번 실행하고 끝나는 시스템이 아니라 **원하는 상태가 계속 유지되는지 감시하는 시스템**이다.

## 2. 명령형과 선언형 설계 철학

Kubernetes를 이해하려면 먼저 명령형(Imperative)과 선언형(Declarative)의 차이를 알아야 한다.

### 명령형(Imperative)

명령형 방식은 시스템에게 구체적인 작업을 직접 지시한다.  
ex) Pod 하나 생성해라 Pod 하나 삭제해라 Pod를 다시 시작해라

사용자는 어떻게 수행할지까지 직접 지정한다.

즉 절차 중심 접근 방식이다.

### 선언형(Declarative)

선언형 방식은 원하는 결과만 정의한다.

ex) `spec: replicas: 3`

사용자는 Pod를 어떻게 만들지 언제 만들지 어디에 만들지를 직접 지정하지 않는다. 대신 '항상 Pod 3개 유지'라는 목표만 선언한다. 그 목표를 실제로 달성하는 것은 Kubernetes의 역할이다.

### Kubernetes가 선언형을 사용하는 이유

선언형 모델은 다음과 같은 장점을 가진다.

- 상태 관리가 쉬움
- 자동 복구 가능
- 변경 추적 용이
- GitOps 적용 가능
- 대규모 운영에 적합

그래서 Kubernetes는 선언형 모델을 핵심 철학으로 채택하고 있다.

## 3. Desired State와 Actual State

선언형 시스템에서는 항상 두 가지 상태가 존재한다.

### Desired State

사용자가 원하는 상태 ex) `spec: replicas: 3`

### Actual State

현재 실제 상태 ex) `Pod 2개 실행 중`

이 두 상태는 항상 동일하지 않을 수 있다.

Pod 하나가 장애로 종료되면 이런 상황이 발생할 수 있다. Kubernetes는 이 차이를 감지하고 2개 → 3개로 상태를 복구한다.

즉 Kubernetes의 모든 동작은 **Desired State와 Actual State의 차이를 줄이는 과정**이라고 볼 수 있다.

## 4. Controller 패턴

이 상태 차이를 줄이는 역할을 하는 것이 Controller다. Controller는 Kubernetes 내부의 자동화 관리자라고 생각할 수 있다.

대표적인 Controller 예시

- Deployment Controller
- ReplicaSet Controller
- StatefulSet Controller
- Job Controller

Controller는 계속해서 다음 작업을 반복한다.

<pre>
상태 확인 → 차이 발견 → 수정 → 상태 재확인
</pre>

이 반복 구조를 Control Loop라고 부른다.

### Control Loop

Controller의 핵심 동작 구조는 다음과 같다.

<pre>
Desired State 확인 ↓ Actual State 확인 ↓ 차이 비교 ↓ 상태 수정 ↓ 다시 확인
</pre>

Controller는 지속적으로 시스템 상태를 감시하는 관리자 역할을 수행한다.

## 5. Reconcile의 본질

Reconcile은 Controller의 핵심 동작인 현**재 상태를 원하는 상태로 수렴시키는 과정**이다.

ex) `Desired State Pod 3개 Actual State Pod 1개`

이 상황에서 Reconcile은 Pod 2개 추가 생성을 수행한다.

반대로 `Desired State Pod 3개 Actual State Pod 5개`라면 Pod 2개 제거를 수행한다.

즉 Reconcile은 상태 차이를 해결하는 작업 자체를 의미한다.

### Reconcile은 반복적으로 실행된다

중요한 점은 Reconcile은 한 번 실행되고 끝나는 함수가 아니다.

다음 상황에서 계속 호출된다.

- 리소스 생성
- 리소스 수정
- 리소스 삭제
- 상태 변경
- Controller 재시작

**Kubernetes는 항상 상태를 감시하고 필요한 경우 Reconcile을 수행한다.**

## 6. Self-Healing과 상태 수렴

Kubernetes가 강력한 이유 중 하나는 Self-Healing 능력 때문이다.

Self-Healing이란 **장애가 발생해도 자동으로 원하는 상태를 복구하는 능력**이다.

<pre>
Pod 장애 발생 → Actual State 변경 → Controller 감지 → Reconcile 수행 → 새 Pod 생성
</pre>

결과적으로 사용자는 별도의 개입 없이도 원하는 상태를 유지할 수 있다.

### 상태 수렴(State Convergence)

Kubernetes는 현재 상태를 목표 상태로 계속 수렴시킨다.

<pre>
현재 상태 Pod 1개 목표 상태 Pod 3개
↓
Pod 생성
↓
현재 상태 Pod 3개
</pre>

이 과정을 상태 수렴(State Convergence)이라고 한다. Kubernetes의 모든 Controller는 이 원리를 기반으로 동작한다.
