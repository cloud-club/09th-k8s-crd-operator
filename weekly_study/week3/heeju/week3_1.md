# SDK / Kubebuilder 사용 이유 분석

> - Kubernetes Operator 개발에서 왜 Kubebuilder와 SDK가 사용되는지, 그리고 어떤 문제를 해결하기 위해 등장했는지를 이해합니다.
> - Kubernetes 확장 구조와 Operator 패턴의 흐름까지 함께 학습하는 것을 목표로 합니다.

## 목차

1. [Kubernetes 확장이 필요한 이유](#1-kubernetes-확장이-필요한-이유)
2. [Operator란 무엇인가](#2-operator란-무엇인가)￼
3. [Kubebuilder란 무엇인가](#3-kubebuilder란-무엇인가)￼
4. [왜 Kubebuilder를 사용하는가](#4-왜-kubebuilder를-사용하는가)￼
5. [Kubebuilder를 사용하지 않으면 어떤 문제가 생기는가](#5-kubebuilder를-사용하지-않으면-어떤-문제가-생기는가)
6. [Kubebuilder의 핵심 철학](#6-kubebuilder의-핵심-철학)￼
7. [실무에서 Kubebuilder가 중요한 이유](#7-실무에서-kubebuilder가-중요한-이유)￼

## 1. Kubernetes 확장이 필요한 이유

Kubernetes는 기본적으로 Pod, Deployment, Service 같은 리소스를 제공한다. 하지만 실제 운영 환경에서는 회사나 서비스마다 원하는 관리 대상이 다르다.

예를 들어:

- “DB 인스턴스를 자동 생성하고 싶다”
- “특정 설정이 들어오면 Redis 클러스터를 자동 구성하고 싶다”
- “게임 서버를 자동으로 증설하고 싶다”
- “사내 AI 모델 배포를 Kubernetes 리소스처럼 관리하고 싶다”

이런 요구사항은 기본 Kubernetes 기능만으로는 해결하기 어렵다.

그래서 Kubernetes는 **새로운 리소스를 직접 정의할 수 있게 하고, 그 리소스를 자동으로 관리하는 프로그램을 붙일 수 있게 만들었다.**

이 구조가 바로

- CRD(Custom Resource Definition)
- Controller
- Operator

생태계다.

## 2. Operator

**Kubernetes 안에서 특정 리소스를 자동으로 관리하는 프로그램**  
-> 사람이 반복적으로 수행하던 운영 작업을 코드로 자동화한 것

예시:

- MySQL Operator
- Kafka Operator
- Prometheus Operator
- ArgoCD

이런 것들은 모두 Kubernetes 안에서 동작하는 자동화 관리 시스템이다.

Operator는 리소스 상태 감시, 원하는 상태(desired state) 유지, 자동 복구/생성, 업데이트 관리 장애 대응 등 Kubernetes의 관리를 애플리케이션 수준까지 확장한다.

## 3. Kubebuilder

**Kubernetes Operator 개발을 쉽게 해주는 공식 프레임워크**  
-> CRD + Controller + Webhook 등을 빠르게 개발하도록 도와주는 scaffolding 도구

**<Kubebuilder로 자동화 가능한 것>**

- 프로젝트 구조 자동 생성
- Controller 기본 코드 생성
- CRD YAML 자동 생성
- RBAC 설정 자동 생성
- 테스트 구조 생성
- Webhook 구조 생성

복잡한 Kubernetes API 개발 과정을 템플릿 기반으로 정리해준다.

## 4. 왜 Kubebuilder를 사용하는가

#### 1. Kubernetes API 개발은 매우 복잡하다

Kubernetes Controller를 직접 구현하려면

- API Server 통신
- Informer
- Watch
- Cache
- Event 처리
- Queue
- Reconcile loop
- RBAC
- Scheme 등록
- CRD 생성
- 상태 동기화

등을 모두 이해해야 하기 때문에 초기 진입 장벽이 상당히 높다.

그러나 Kubebuilder는 이 복잡한 구조를 표준 형태로 자동 생성해주기 때문에 **개발자는 비즈니스 로직에 집중할 수 있다.**

#### 2. Kubernetes 권장 구조를 따르게 된다

Kubebuilder는 Kubernetes SIG API Machinery 팀이 권장하는 구조를 기반으로 만들어졌다.

따라서 프로젝트 구조, API 버전 관리, Controller 작성 방식 등이 Kubernetes 표준 패턴과 일치하기 때문에 **유지보수성, 협업 효율, 업그레이드 대응에 좋다.**

#### 3. 코드 생성 자동화

Kubebuilder는 코드 생성 기능이 강력하다.

`kubebuilder create api --group app --version v1 --kind GameServer`

이 명령 하나로 API 타입, Controller, CRD, 테스트 코드, RBAC 설정 등 반복 작업을 줄이고 실수를 줄일 수 있다.

## 5. Kubebuilder를 사용하지 않으면 어떤 문제가 생기는가

직접 구현할 경우 프로젝트 구조 설계, API 등록 직접 구현, Scheme 연결, Watch 처리, Queue 구현, Cache 동기화 구현, YAML 생성 등이 필요하다.

특히 Kubernetes의 비동기 이벤트 구조는 상당히 복잡하기 때문에 실무에서는 Kubebuilder, Operator SDK 같은 프레임워크를 사용한다.
