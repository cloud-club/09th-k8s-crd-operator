# Operator 고도화 — Status & Finalizer

> - Kubernetes Operator가 단순히 리소스를 생성·수정하는 수준을 넘어, 현재 상태를 사용자에게 제공하고 리소스 삭제 과정까지 안전하게 관리하는 방법을 학습합니다.
> - 특히 Status 업데이트 전략과 Finalizer 패턴을 통해 실무적인 Operator 설계 방식을 이해하는 것을 목표로 합니다.

## 목차

1. Operator 고도화란 무엇인가
2. Status란 무엇인가
3. Status 업데이트 전략
4. Finalizer란 무엇인가
5. Finalizer 패턴 동작 과정
6. Garbage Collection(GC)과 OwnerReference

# 1. Operator 고도화란 무엇인가

초기 단계의 Operator는 보통 리소스 조회, 생성, 수정 기능만 수행한다. 하지만 실제 운영 환경에서는 다음과 같은 요구사항이 발생한다.

- 현재 상태를 사용자에게 보여주기
- 삭제 전에 외부 자원 정리하기
- 장애 상태 기록하기
- 진행 상황 표시하기

이를 위해 사용하는 대표적인 기능이 Status와 Finalizer이다.

# 2. Status란 무엇인가

Kubernetes 리소스는 일반적으로 두 영역으로 나뉜다.

<pre>
spec:
status:
</pre>

## Spec

사용자가 원하는 상태

<pre>
spec:
    replicas: 3
</pre>

의미: Pod 3개를 유지하고 싶다

## Status

현재 실제 상태

<pre>
status:
    availableReplicas: 2
    phase: Running
</pre>

의미: 현재 Pod 2개가 실행 중이다

## 왜 Status가 필요한가

사용자는 다음과 같은 정보를 알고 싶다.

ex) 현재 정상 동작 중인가? 배포는 완료되었는가? 에러가 발생했는가?

이 정보를 Status에 기록한다.

따라서 Status는 **Controller가 사용자에게 전달하는 현재 상태 보고서**라고 볼 수 있다.

# 3. Status 업데이트 전략

Operator는 Reconcile 과정에서 상태를 계산하고 Status를 갱신한다.

ex)

<pre>
Deployment 생성
 ↓ 
Pod 생성 완료 확인
 ↓
Status 업데이트
</pre>

## Status는 Spec과 분리된다

`spec`과 `status`는 서로 다른 영역이다.

사용자는 보통 Spec을 수정한다. 반면 Controller는 Status를 수정한다.

<pre>
- 사용자 → Spec 관리
- Controller → Status 관리
</pre>

이렇게 역할이 분리된다.

## Status를 사용하는 이유

Status를 사용하면

- 현재 상태 확인 가능
- 장애 원인 파악 가능
- 진행 상황 표시 가능
- 운영 편의성 향상

효과를 얻을 수 있다. 실제 Operator 대부분은 Status를 적극적으로 활용한다.

# 4. Finalizer란 무엇인가

기본적으로 Kubernetes 리소스를 삭제하면(`kubectl delete`) 즉시 삭제된다.

하지만 다음과 같은 상황이 있을 수 있다.

ex)

<pre>
CR 삭제
 ↓
외부 DB는 아직 존재
----------------------------------
CR 삭제
 ↓
Cloud LoadBalancer 남아 있음
</pre>

이 경우 단순 삭제하면 리소스가 남게 된다. 이를 해결하기 위해 Finalizer를 사용한다.

## Finalizer의 역할

Finalizer는 **삭제 전에 반드시 수행해야 하는 작업을 보장하는 메커니즘**이다.

<동작 순서>

<pre>
삭제 요청
 ↓
정리 작업 수행
 ↓
Finalizer 제거
 ↓
실제 삭제
</pre>

# 5. Finalizer 패턴 동작 과정

일반적인 흐름은 다음과 같다.

### 1단계

리소스 생성 시 Finalizer 등록

<pre>
CR 생성
 ↓
Finalizer 추가
</pre>

### 2단계

사용자가 삭제 요청

<pre>kubectl delete</pre>

### 3단계

Kubernetes는 즉시 삭제하지 않는다. 대신 **DeletionTimestamp** 설정을 수행한다.

### 4단계

Operator가 삭제 이벤트 감지

<pre>
삭제 요청 확인
 ↓
외부 자원 정리
</pre>

- Cloud 리소스 삭제
- 데이터베이스 삭제
- Secret 정리

### 5단계

정리가 끝나면 Finalizer 제거

<pre>
Finalizer 제거
 ↓
실제 삭제
</pre>

-> Finalizer는 안전한 삭제를 보장하기 위한 장치다.

# 6. Garbage Collection(GC)과 OwnerReference

Kubernetes에는 Garbage Collection 기능이 존재한다.

GC는 **더 이상 필요 없는 리소스를 자동 삭제**하는 기능이다.

## OwnerReference

리소스 간 소유 관계를 정의한다.

<pre>
GameServer
 └── Deployment
      └── Pod
</pre>

여기서 Deployment의 Owner를 GameServer로 설정할 수 있다.

즉 Deployment는 GameServer가 소유한다라는 의미다.

## GC 동작

부모 리소스가 삭제되면

<pre>
GameServer 삭제
 ↓
Deployment 삭제
 ↓
Pod 삭제
</pre>

가 자동 수행된다. 이를 Garbage Collection이라고 한다.

## Finalizer와 GC 차이

### GC

부모 삭제 시, 자식 자동 삭제

### Finalizer

<pre>
삭제 요청
 ↓
사용자 정의 정리 작업
 ↓
실제 삭제
</pre>

=> **GC는 Kubernetes가 자동 처리하고, Finalizer는 Operator가 직접 처리한다.**
