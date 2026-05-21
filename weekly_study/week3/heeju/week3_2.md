# Scaffolding 프로젝트 구조 분석

> - Kubebuilder가 생성하는 프로젝트 구조를 분석하고, 각 디렉토리와 파일이 어떤 역할을 수행하는지 이해합니다.
> - Kubernetes Controller 프로젝트가 왜 특정 구조를 따르는지도 함께 학습합니다.

## 목차

1. Scaffolding이란 무엇인가
2. 왜 프로젝트 구조가 중요한가
3. Kubebuilder 기본 프로젝트 구조
4. api / controllers 디렉토리 분석
5. Reconcile과 상태 동기화
6. 왜 Scaffolding이 중요한가
7. 실무 관점에서의 의미

## 1. Scaffolding

**프로젝트의 기본 구조를 자동 생성하는 것**  
-> 초기 개발 구조를 빠르게 세팅해주는 개념

Kubebuilder에서는

<pre>
kubebuilder init
kubebuilder create api
</pre>

같은 명령으로 프로젝트 뼈대를 자동 생성해서 개발자가 Kubernetes Controller의 복잡한 구조를 처음부터 직접 만들지 않아도 되도록 도와준다.

## 2. 왜 프로젝트 구조가 중요한가

Kubernetes Controller 프로젝트는 일반 애플리케이션보다 훨씬 복잡하다.

- API 타입 관리 필요
- 버전 관리 필요
- Controller 관리 필요
- CRD 관리 필요
- RBAC 관리 필요
- Webhook 관리 필요
- 상태 동기화 필요

등 다양한 요소가 존재하기 때문이다.

구조가 정리되지 않으면 유지보수 어려움, API 충돌, 버전 혼란, 테스트 복잡성 증가 같은 문제가 발생한다. 그래서 Kubebuilder는 Kubernetes 권장 구조를 기본으로 제공한다.

-> **개발자가 일관된 방식으로 Controller를 개발할 수 있게 만든다.**

## 3. Kubebuilder 기본 프로젝트 구조

Kubebuilder 프로젝트는 보통 다음 구조를 가진다.

<pre>
project/
├── api/
├── cmd/
├── config/
├── controllers/
├── internal/
├── hack/
├── test/
├── main.go
├── Makefile
└── PROJECT
</pre>

각 디렉토리는 명확한 역할을 가진다.

- api → Custom Resource 타입 정의
- controllers → Reconcile 로직 구현
- config → Kubernetes YAML 관리
- cmd / main.go → 애플리케이션 실행 진입점

등으로 역할이 분리되어 유지보수성 향상, 협업 효율 증가, Kubernetes 표준 패턴 유지를 위해 매우 중요하다.

## 4. api / controllers 디렉토리 분석

4-1. api 디렉토리

<pre>
api/
└── v1/
</pre>

이곳은 Custom Resource 정의를 위한 Go 타입이 들어간다.

<pre>
type GameServerSpec struct {}
type GameServerStatus struct {}
</pre>

이 타입들은 Kubernetes API 리소스 구조를 정의한다.  
(YAML로 작성되는 Custom Resource의 스키마를 정의하는 영역)

Kubernetes 리소스는 보통 Spec, Status로 나뉜다.

#### Spec

: 사용자가 원하는 상태

- replica 개수
- 이미지 버전
- 설정값

#### Status

: 현재 실제 상태

- 실행 중 여부
- 현재 Pod 개수
- 에러 상태

Controller는 Spec를 보고 Status를 맞춰가는 역할을 한다.

## controllers 디렉토리

<pre>
controllers/
└── gameserver_controller.go
</pre>

리소스 감시, 상태 비교, 생성/수정/삭제, 자동 복구 등 핵심 로직이 수행된다.

**Controller는 지속적으로 리소스 상태를 감시하면서 원하는 상태를 유지한다.**

## 5. Reconcile과 상태 동기화

Kubernetes Controller의 핵심은 Reconcile loop다.
-> 현재 상태가 원하는 상태와 다르다면 상태를 맞춘다!

Kubernetes는 단순 실행 시스템이 아니라 지속적으로 상태를 수렴시키는 선언형 시스템이다.

## 6. 왜 Scaffolding이 중요한가

**<Scaffolding이 중요한 이유>**

- 표준 구조 제공
- 반복 작업 제거
- Kubernetes 규칙 자동 적용
- 생산성 향상
- 협업 효율 증가

Kubernetes 생태계는 구조가 복잡하기 때문에 초기 구조 자동화의 가치가 매우 크다.  
-> **개발자가 Kubernetes 내부 구조보다 비즈니스 로직에 더 집중할 수 있게 해준다.**

## 7. 실무 관점에서의 의미

실무에서는 여러 Controller 존재, 여러 API 버전 존재, 테스트 필요, 운영 자동화 등 프로젝트 규모가 커진다.

Scaffolding 없이 직접 구조를 설계하면 일관성 붕괴, 유지보수 비용 증가, API 충돌과 같은 문제가 발생한다.

Kubebuilder는 이 문제를 해결하기 위해 Kubernetes 권장 구조를 강제한다.  
-> 현대 Kubernetes Operator 개발에서 사실상 표준 개발 방식 중 하나로 사용된다.
