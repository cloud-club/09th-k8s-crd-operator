# Reconcile 패턴 구현 — Reconcile 함수 / 리소스 관리 / OwnerReference

> - Kubernetes Controller의 핵심인 Reconcile 패턴을 이해하고, 실제 Controller 내부에서 리소스를 조회·생성·업데이트하는 흐름을 학습합니다.
> - 또한 OwnerReference를 통해 Kubernetes 리소스 간 소유 관계를 어떻게 관리하는지도 함께 이해합니다.

## 목차

1. Reconcile 패턴이란 무엇인가
2. Reconcile() 함수의 구조
3. 리소스 조회(Get) 로직
4. 리소스 생성(Create) 로직
5. 리소스 업데이트(Update) 로직
6. Ownership와 OwnerReference
7. Reconcile 구현 시 중요한 개념

---

# 1. Reconcile 패턴이란 무엇인가

Kubernetes Controller의 핵심은 Reconcile loop다.

<pre>현재 상태 != 원하는 상태
→ 상태를 맞춤</pre>을 반복적으로 수행한다.<br><br><br>

원하는 상태: `replicas: 3`

현재 상태: `Pod 1개만 실행 중`

일 때 Controller는

- 현재 상태 조회
- 부족한 리소스 생성
- 상태 재확인
- 원하는 상태 유지

를 반복한다.

=> Kubernetes는 단순 실행 시스템이 아니라 지속적으로 상태를 수렴시키는 선언형 시스템이다.

# 2. Reconcile() 함수의 구조

Controller의 핵심 로직은 `Reconcile()` 함수 안에서 동작한다.

**<일반적인 흐름>**

<pre>
1. Custom Resource 조회
2. 현재 상태 확인
3. 관련 리소스 조회
4. 원하는 상태와 비교
5. 생성 / 수정 / 삭제 수행
6. Status 업데이트
7. 종료 또는 재시도
</pre>

Reconcile() 함수는 **현재 상태를 원하는 상태로 맞추는 작업 흐름** 전체를 담당한다.

<특징>

- 이벤트 기반 호출
- 반복 실행 가능
- 멱등성(Idempotency) 유지 필요
- 실패 시 Retry 가능

# 3. 리소스 조회(Get) 로직

Controller는 먼저 현재 상태를 확인해야 한다. 그래서 가장 먼저 수행하는 작업이 조회(Get)다.

ex) Custom Resource 조회, Deployment 존재 여부 확인, Pod 상태 확인, ...

<조회 흐름>

<pre>
Kubernetes API
→ 현재 리소스 상태 조회
→ 원하는 상태와 비교
</pre>

Kubernetes Controller는 무조건 생성”하는 시스템이 아니라 현재 상태를 기반으로 판단하는 시스템이다.
=> 이미 존재하면 생성하지 않고, 필요할 때만 수정해야 한다.

# 4. 리소스 생성(Create) 로직

조회 결과 원하는 리소스가 존재하지 않으면 생성(Create)한다.

<pre>Deployment 없음
→ 새 Deployment 생성</pre>

Controller는 보통 다음 흐름으로 동작한다.

<pre>리소스 조회
→ 존재 여부 확인
→ 없으면 생성</pre>

이 방식은 중복 생성 방지에 중요하다.

Kubernetes Controller는 반복적으로 실행되므로 매번 새 리소스를 만들면 안 된다. 따라서 반드시 현재 상태를 먼저 조회한 뒤 생성해야 한다.

# 5. 리소스 업데이트(Update) 로직

리소스가 이미 존재하지만 원하는 상태와 다르면 수정(Update)한다.

원하는 상태: `replicas: 5`

현재 상태: `replicas: 3`

이 경우 Controller는 기존 리소스 조회, 차이 비교, 필요한 부분 수정 이렇게 수행한다.

즉 Reconcile의 핵심은 **없는 걸 만들기보다는 현재 상태와 원하는 상태의 차이를 줄이는 것**에 가깝다.

# 6. Ownership와 OwnerReference

Kubernetes에서는 리소스 간 소유 관계를 설정할 수 있다. 이때 사용하는 것이 OwnerReference다.

<pre>
GameServer CR
└── Deployment
    └── Pod
</pre>

이 구조에서 Deployment의 owner를 GameServer로 설정할 수 있다.

의미는 이 Deployment는 GameServer가 관리한다는 뜻이다.

## OwnerReference의 장점

### 자동 정리(Garbage Collection)

부모 리소스 삭제 시 자식 리소스도 자동 삭제된다.

ex) GameServer 삭제 → Deployment 자동 삭제 → Pod 자동 삭제

### Controller 소유권 명시

어떤 Controller가 어떤 리소스를 관리하는지 Kubernetes가 이해할 수 있다.

즉 리소스 관계를 명확하게 표현할 수 있다.

### Watch 연결 가능

OwnerReference가 연결되면 자식 리소스 변경 시 부모 Reconcile을 다시 호출할 수 있다.  
-> 상태 동기화가 쉬워진다.

# 7. Reconcile 구현 시 중요한 개념

## 7-1. 멱등성(Idempotency)

Reconcile은 여러 번 호출될 수 있다. 따라서 몇 번 실행해도 결과가 동일해야 한다.

ex)

- 이미 존재하는 리소스를 또 생성하면 안 됨
- 상태가 같으면 수정하지 않아야 함

## 7-2. Desired State 중심 사고

Controller는 명령형이 아니라 선언형 방식으로 동작한다.

명령형: `Pod 하나 생성해라`

선언형: `Deployment 없음 → 새 Deployment 생성`

Reconcile은 현재 상태를 Desired State(원하는 상태)로 계속 수렴시키는 구조다.

## 7-3. 반복 실행 가능한 구조

Kubernetes 환경은 Pod 삭제, 노드 장애, 사용자 수정, API 오류 등으로 항상 변한다.
따라서 Reconcile은

- **반복 실행** 가능해야 하고
- **실패 복구** 가능해야 하며
- **상태 기반으로 동작**해야 한다.

이것이 Kubernetes Controller 패턴의 핵심이다.
