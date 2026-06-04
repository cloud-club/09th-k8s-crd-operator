# Client-go의 이해

> - Kubernetes Controller가 실제로 어떻게 동작하는지 이해하기 위해 Client-go의 핵심 컴포넌트를 학습합니다.
> - 특히 Informer, Lister, Workqueue가 어떤 역할을 수행하며 왜 필요한지 이해하는 것을 목표로 합니다.

## 목차

1. Client-go란 무엇인가
2. Kubernetes Controller와 Client-go
3. Informer
4. Lister
5. Workqueue
6. Client-go 기반 Controller 동작 흐름

## 1. Client-go란 무엇인가

Client-go는 **Go 프로그램이 Kubernetes API Server와 통신할 수 있도록 해주는** Kubernetes 공식 Go 클라이언트 라이브러리이다.

- Pod 조회
- Deployment 생성
- Service 수정
- Custom Resource 조회

등의 작업을 수행할 수 있다.

하지만 Client-go는 단순 API 호출 라이브러리가 아니다. Kubernetes Controller를 구현하기 위한 다양한 기능도 함께 제공한다.

ex) Informer, Lister, Workqueue

이 세 가지는 Kubernetes Controller의 핵심 구성 요소다.

## 2. Kubernetes Controller와 Client-go

Controller는 단순히 API를 호출하는 프로그램이 아니다.

`리소스 변경 감지 → 상태 조회 → 이벤트 처리 → Reconcile 수행`

만약 매번 API Server에 요청한다면 Pod 조회 Pod 조회 Pod 조회 Pod 조회...와 같은 방식이 되어 API Server 부하가 매우 커진다. 그래서 Client-go는 이벤트 감지, 로컬 캐시, 작업 큐를 제공한다.

-> Controller가 효율적으로 동작하도록 돕는 기반 라이브러리

## 3. Informer

Informer는 Kubernetes 리소스 변경을 감시하는 컴포넌트다. **API Server의 변경 이벤트를 감지하고 로컬 캐시에 저장하는 역할**을 한다.

ex) Pod 생성, Pod 삭제, Deployment 수정, Custom Resource 변경 등

Informer는 Kubernetes Watch API를 사용한다.

동작 흐름:

<pre>
API Server
 ↓
Watch
 ↓
Informer
 ↓
Local Cache
</pre>

변경 이벤트를 지속적으로 수신하면서 최신 상태를 캐시에 유지한다.

### 왜 Informer가 필요한가

매번 API Server에 요청하면 Controller → API Server 방식으로 동작한다. 이것은 대규모 클러스터에서는 매우 비효율적이다.

Informer는 아래와 같은 구조를 사용한다.

<pre>
API Server
 ↓
Informer
 ↓
Cache
 ↓
Controller
</pre>

따라서 API 호출 감소, 성능 향상, 네트워크 사용 감소효과를 얻을 수 있다.

## 4. Lister

Lister는 Informer가 관리하는 캐시를 조회하기 위한 객체다. **캐시에 저장된 데이터를 빠르게 조회하는 역할**을 한다.

ex) Pod 목록 조회, Deployment 조회, Custom Resource 조회 등

중요한 점은 Lister는 API Server를 직접 조회하지 않는다. 조회 대상은 항상 Local Cache이다.

동작 구조:

<pre>
Controller
 ↓
Lister
 ↓
Local Cache
</pre>

## Lister를 사용하는 이유

API Server 직접 조회: `Controller → API Server`

캐시 조회: `Controller → Lister → Cache`

캐시 조회가 훨씬 빠르기 때문에 **Controller는 대부분 Lister를 통해 리소스를 조회**한다.

## 5. Workqueue

Workqueue는 **처리해야 할 작업을 저장하는 큐**다. 이벤트를 안전하게 처리하기 위한 작업 대기열 역할을 한다.

<pre>
Pod 생성 이벤트 발생
 ↓
Workqueue 등록
 ↓
Controller 처리
</pre>

### 왜 Queue가 필요한가

이벤트가 한 번에 많이 발생할 수 있다. (ex. Pod 100개 생성) 만약 이벤트를 즉시 처리하면 `이벤트 → 즉시 처리 → 즉시 처리 → 즉시 처리`가 되어 Controller가 과부하될 수 있다.

Workqueue는 `이벤트 → Queue 저장 → 순차 처리` 방식으로 동작한다.

### Retry 지원

Controller 작업은 API 오류, 네트워크 장애, 리소스 충돌 등의 이유로 실패할 수 있다. Workqueue는 실패한 작업을 다시 등록할 수 있다.

<pre>
작업 실패
 ↓
Queue 재등록
 ↓
재시도
</pre>

이로써 Controller 안정성을 높여준다.

## 6. Client-go 기반 Controller 동작 흐름

Client-go 기반 Controller는 일반적으로 다음 순서로 동작한다.

<pre>
리소스 변경 발생
 ↓
Informer가 이벤트 감지
 ↓
캐시 갱신
 ↓
Workqueue 등록
 ↓
Controller 처리
 ↓
Lister를 통해 상태 조회
 ↓
Reconcile 수행
</pre>

조금 더 자세히 표현하면

<pre>
API Server
 ↓ 
Watch
 ↓ 
Informer
 ↓ 
Local Cache
 ↓ 
Workqueue
 ↓ 
Controller
 ↓ 
Lister
 ↓ 
Reconcile
</pre>

이 구조 덕분에 Kubernetes는 수많은 리소스를 효율적으로 관리할 수 있다.
