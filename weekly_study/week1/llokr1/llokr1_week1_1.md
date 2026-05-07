# 1주차 - CRD와 오퍼레이터 패턴

## Kubernetes의 API Server 구조

오퍼레이터 패턴 및 CRD 등의 개념을 공부하기 전, 쿠버네티스의 API Server 구조에 대해 알아야 한다.

뒤에서 자세히 정리하겠지만 CRD는 기본적으로 이 Kubernetes의 API를 확장하는 개념이기 때문이다.

#### API Server

API Server는 쿠버네티스의 컨트롤 플레인에 존재하는 컴포넌트이다. 

이러한 API 서버는 etcd의 데이터를 저장 및 조회함으로써 쿠버네티스 오브젝트들의 상태를 조회 및 제어할 수 있고, 클러스터의 다른 컴포넌트와 서로 통신할 수 있다. 제공하는 API는 HTTP이다.

#### API 활용

API를 활용하여 통신하는 방법은 여러가지가 있다.

먼저 `kubectl`은 리소스를 조회 및 제어할 수 있는 대표적인 CLI 도구이다.

`kubectl get <리소스 이름>` 과 같은 명령을 통해 제어할 수 있는데, 이러한 명령도 결국 내부적으로는 API를 호출하는 것이다.

또한 애플리케이션 내부에서 쿠버네티스 API를 사용하거나, 코드 레벨에서 쿠버네티스 리소스를 제어하는 경우도 있는데, 이때 [클라이언트 라이브러리](https://kubernetes.io/ko/docs/reference/using-api/client-libraries/)를 활용할 수 있다.

#### API 구조

Manifest 파일을 선언할 때, 대부분 아래와 같이 최상단에 `apiVersion` 을 선언한다.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
```

일반적으로 API는 `{api_group}/{version}` 으로 구성된다.

- **API 그룹**이란, **리소스를 논리적으로 묶는 단위**를 의미한다.
    
    즉, 위 예시는 ‘apps’라는 API 그룹의 v1 버전의 `deployment` 리소스를 사용한다는 의미가 된다.
    
    API 그룹은 리소스를 기능별로 분리하거나, **확장성을 확보**하기 위해 사용되는 개념이다.
    
- **버전**을 명시함으로써 **리소스의 안정성과 지원의 차이**를 나타낸다.
    
    [API 변경 문서](https://github.com/kubernetes/community/blob/main/contributors/devel/sig-architecture/api_changes.md#alpha-beta-and-stable-versions)를 참고하면 버전 명시에 대한 더 많은 정보를 찾을 수 있는데, 기본적인 명시 기준은 아래와 같다.
    
    - **알파(Alpha) :**
        
        Ex) `v1alpha1`
        
        버그가 있을 수도 있으며, 언제든 지원 중단될 수 있는 버전이다.
        
    - **베타(Beta) :**
        
        Ex) `v2beta3` 
        
        테스트가 잘 돼있고 안정성도 보장되지만, 추후 기능 변경 시 호환성에 문제가 발생할 수 있으므로 프로덕션 환경에서의 사용은 권장되지 않는다.
        
    - **안정화(Stable):**
        
        Ex) `v1` 
        
        안정성, 호환성, 장기 지원 모두 가능한 안정화된 버전이다.
        

---

## 오퍼레이터 패턴이란

오퍼레이터 패턴은 **컴포넌트를 관리하는 운영자의 역할을 소프트웨어 레벨로 구현하는 패턴이다**.

즉, 사용자 정의 리소스(CRD)를 사용하여 CRD는 Custom Resource Definition, 용어 그대로 **정의**를 하는 것이라면, 오퍼레이터는 이렇게 정의된 리소스를 실제로 동작하게 하는 **컨트롤러 패턴**이라고 볼 수 있다.

1. CRD를 정의하면
2. CRD가 API 서버에 저장된다.
3. 그리고 실제 Custom Resource가 생성되면
4. Operator가 해당 Custom Resource를 감지하게 되고,
5. **Reconciliation Loop**을 통해 사용자가 선언한 상태로 유지한다.

> **Reconciliation Loop**란, 실제 상태를 원하는 상태로 맞추는 루프를 의미한다.

### 오퍼레이터 vs 컨트롤러

오퍼레이터와 컨트롤러는 무슨 차이가 있을까? 

**오퍼레이터 또한 컨트롤러**라고 볼 수 있다. 컨트롤러란, 특정 리소스의 상태를 감시하며 원하는 상태에 맞추기 위한 행동을 의미한다.

다만, 오퍼레이터는 컨트롤러에 비해 **도메인 지식**이 포함되고, 장애가 발생했을 때, 백업 및 복구가 필요할 때 등, 기본적인 컨트롤러보다 컴포넌트의 운영 방식을 더 세부적으로 선언할 수 있다는 것이 특징이다.

따라서 오퍼레이터는 컨트롤러를 기반으로 보다 세부적인 도메인과 스케줄링 로직을 구현할 수 있다는 것이 특징이다.

---

## CRD

CRD(Custom Resource Definition)는 기본 쿠버네티스 API의 확장형 엔드포인트이다.

다르게 표현한다면, **Kubernetes API에 새로운 리소스 타입을 추가하는 선언**이라고 볼 수 있다.

본질적인 목표는 Deployment, ConfigMap과 같이 쿠버네티스에서 제공하는 오브젝트 이외에도 사용자가 별도의 리소스를 정의하여 오브젝트로서 관리하고자 할 때 사용되는 개념이다.

CRD는 오퍼레이터 패턴을 구현할 때는 **이상적인 리소스의 상태를 표현하는 데 사용**된다.

#### CRD와 CR

CRD와 CR은 **선언된 리소스**와 **생성된 인스턴스**의 관계이다.

아래의 매니페스트 파일을 예시로 든다면, 쿠버네티스 기본 리소스에는 `MyApp` 이라는 리소스가 없기 때문에 이 `kind`는 사용자가 정의한 리소스라는 것을 알 수 있다.

```yaml
apiVersion: stable.example.com/v1
kind: MyApp
metadata:
  name: my-app
spec:
  replicas: 3
```

그리고 이 `MyApp` 타입으로 리소스의 인스턴스를 생성한다면 위와 같은 형식으로 매니페스트 파일은 선언할 수 있을 것이다. 이때, `MyApp` 은 **CRD**로 정의된 kind가 되고, `MyApp` 의 세부적인 spec을 선언하여 생성된 인스턴스가 **CR**이라고 볼 수 있다.

(기본 리소스로 비유하자면, **CRD**와 **CR**은 ‘**deployment’** 와 ‘**deployment 를 활용하여 선언한 매니페스트 파일’**의 관계와 같다.)

---

## 쿠버네티스 API Aggregation Layer

CRD를 통해서 Kubernetes API를 확장할 수도 있지만, API를 확장하는 또다른 방법으로는 API Aggregation Layer가 있다.

이는 리소스를 추가로 생성하는 것이 아닌, **별도의 API 서버를 붙임**으로써 커스텀 API를 사용할 수 있는 방식이다.

#### CRD와의 차이점

CRD는 클러스터의 API 서버가 새로운 종류의 리소스를 인식하게 하는 것이다.

반면, API Aggregation의 경우, 클러스터의 API 서버 자체에 **커스텀 서버를 붙여 확장하는 방식**이라고 볼 수 있다.

따라서 CRD + 오퍼레이터 패턴보다 더 정교하게 커스텀 리소스들을 관리할 수 있지만, 그만큼 API 서버를 구현하는 데에 복잡함이 있고, 운영하는 데에도 부담이 발생한다는 단점이 있다.

---

## 정리

사용자가 직접 정의한 리소스 타입을 **CRD**(Custom Resource Definition)라고 하고,

이 CRD 리소스의 세부적인 spec을 선언하여 생성된 객체를 **CR**(Custom Resource)라고 한다.

그리고 이렇게 생성된 CR 객체를 원하는 상태로 맞춰가는 것이 **오퍼레이터 패턴**이다.

---

**참고 자료**

https://kubernetes.io/ko/docs/concepts/overview/kubernetes-api/

https://kubernetes.io/ko/docs/reference/using-api/

https://kubernetes.io/ko/docs/concepts/extend-kubernetes/api-extension/custom-resources/

https://kubernetes.io/ko/docs/concepts/extend-kubernetes/api-extension/apiserver-aggregation/

https://dev.gmarket.com/65