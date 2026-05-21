# Controller-runtime 라이브러리 이해

> - controller-runtime 라이브러리가 Kubernetes Controller 내부에서 어떤 역할을 수행하는지 이해합니다.
> - Watch, Queue, Cache, Reconcile 같은 핵심 개념을 중심으로 Kubernetes Controller 동작 원리를 학습합니다.

## 목차

1. controller-runtime이란 무엇인가
2. 왜 controller-runtime이 필요한가
3. Kubernetes Controller의 핵심 흐름
4. Reconcile과 Desired State
5. Manager · Client · Cache 구조
6. Queue와 Retry 메커니즘
7. 실무에서 중요한 이유

## 1. controller-runtime

**Kubernetes Controller 개발을 쉽게 하기 위한 Go 라이브러리**  
-> Kubernetes Controller 개발용 고수준 프레임워크

- Kubebuilder는 프로젝트 구조 생성 도구
- controller-runtime은 실제 Controller 동작 라이브러리

controller-runtime은 Kubernetes Controller 개발에 필요한 복잡한 기능들을 추상화해서 제공한다.

## 2. 왜 controller-runtime이 필요한가

Kubernetes Controller를 직접 구현하려면

- Watch 처리
- Informer 구성
- Shared Cache 관리
- Queue 처리
- Event 처리
- API Client 구현
- Retry 로직
- Reconcile loop

등을 모두 직접 구현해야 해서 매우 복잡하다. controller-runtime은 이런 기능들을 미리 구현해서 제공한다.

개발자는 어떤 리소스를 감시할지, 상태가 달라졌을 때 무엇을 할지에 집중할 수 있다.

## 3. Kubernetes Controller의 핵심 흐름

Kubernetes Controller는 이벤트 기반으로 동작한다.

전체 흐름:

<pre>
리소스 변경 발생
→ 이벤트 감지
→ Queue 저장
→ Reconcile 호출
→ 상태 비교
→ 원하는 상태로 수정
</pre>

- Pod 생성
- Deployment 수정
- Custom Resource 변경

같은 이벤트가 발생하면, controller-runtime이 이를 감지하고 Reconcile을 호출한다.  
-> **Kubernetes는 지속적으로 현재 상태를 원하는 상태에 맞추는 구조로 동작한다.**

- Pod 생성
- Deployment 수정
- Custom Resource 변경

같은 이벤트가 발생하면 controller-runtime이 이를 감지하고 Reconcile을 호출한다.

-> **Kubernetes는 지속적으로 현재 상태를 원하는 상태에 맞추는 구조로 동작한다.**

4. Reconcile과 Desired State

#### Reconcile

현재 상태를 원하는 상태로 수렴시키는 과정

Controller는 부족한 Pod 생성, 상태 다시 확인, 목표 상태 유지를 반복 수행한다.  
-> 이 구조를 **Reconcile loop**라고 부른다.

#### Desired State

사용자가 원하는 상태

#### Actual State

실제 현재 상태

Controller는 이 둘의 차이를 줄이는 역할을 한다.

Kubernetes는 선언형(Declarative) 시스템이다.

## 5. Manager · Client · Cache 구조

#### Manager

controller-runtime의 중앙 관리 객체

**<주요 역할>**

- Controller 관리
- Cache 관리
- Client 관리
- Scheme 등록
- Health check 관리

-> 전체 Controller 시스템을 운영하는 관리자 역할

#### Client

Kubernetes API와 통신하기 위한 객체

**<가능한 작업>**

- Get
- Create
- Update
- Delete

-> Controller가 Kubernetes 리소스를 직접 조작할 수 있게 해준다.

#### Cache

Kubernetes API Server에 직접 계속 요청하면 부하가 커지기 때문에 controller-runtime은 Cache를 사용한다.

<구조>
API Server
→ Informer
→ Local Cache
→ Controller 사용

대부분 조회 작업은 로컬 캐시에서 수행된다.

## 6. Queue와 Retry 메커니즘

이벤트가 발생할 때마다 바로 처리하지 않고 중간 Queue에 저장한다.

이유:

- 이벤트 폭주 방지
- 순차 처리 가능
- 안정성 증가
- Retry 지원

controller-runtime은 비동기 이벤트 시스템을 안정적으로 관리한다.

Controller 작업은 API 오류, 네트워크 장애, 리소스 충돌 등으로 실패할 수 있다.

controller-runtime은 실패 시 재시도, backoff, queue 재등록 등을 자동 처리한다.

## 7. 실무에서 중요한 이유

현대 Kubernetes 운영에서는 자동화가 중요하다.  
controller-runtime은 이런 Kubernetes 자동화 구조의 핵심 기반 중 하나다.

특히 Operator 개발, Custom Controller 개발, Kubernetes 플랫폼 개발
에서는 사실상 표준 라이브러리처럼 사용되기 때문에 Kubernetes 심화 개발을 공부하려면 반드시 이해해야 하는 핵심 기술이다.
