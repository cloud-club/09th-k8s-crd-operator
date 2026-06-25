# Kafka Topic Operator - Week8

## 사전 준비

가비아 클라우드의 team-2 namespace에 미리 띄어놓은 리소스는 다음과 같다.

- Kafka 단일 클러스터

![alt text](image.png)

- 오퍼레이터

![alt text](image-1.png)

아직 CR은 등록하지 않은 상태이다.

![alt text](image-2.png)

---

## 시나리오 1: 토픽 생성

#### CR 이름은 Demo, 토픽 이름은 demo-topic

![alt text](image-3.png)

### describe로 CR 상태 확인

![alt text](image-4.png)

![alt text](image-5.png)

---

## 시나리오 2: 파티션 증가

#### Spec의 partitions를 3에서 6으로 수정

![alt text](image-6.png)

#### describe로 파티션 수 확인

![alt text](image-7.png)

![alt text](image-8.png)

- Spec의 partitions가 6으로 잘 반영이 되어있다.
- Status의 Observed Partitions도 6으로 reconcile에 성공했다
- Spec 수정으로 인해 Generation, Observed Generation이 증가하여 2가 되었다
- conditions도 `Ready=True (TopicSynced)` ,`ConfigDrift=False (InSync)` 로  잘 떠있다

---

## 시나리오 3: 파티션 감소 거부

Kafka는 원칙적으로 파티션 감소를 금지한다. 감소 요청을 보내더라도 에러를 반환한다.

- 파티션 감소를 금지하는 이유
    - 메시지 순서 보장(Ordering)의 파괴
    - 데이터 유실 및 이관(Migration) 비용이 너무 크다
    - 컨슈머 오프셋(Offset) 관리의 한계

- 참고: 그럼 늘리는 것은 왜 허용하는가?
    - 과거 데이터를 Migration 하지 않기 때문에 부하가 없다
    - 동일 파티션 내 메시지 순서 보장 원칙은 깨진다
    - 허용은 하지만 가능한 늘리지 않는 것이 좋다

> 목표:  에러 루프 없이 condition으로 표시하기 (에러 반환하면 무한루프)

#### Spec의 partitions값을 6에서 2로 수정

![alt text](image-9.png)

#### describe로 파티션 수 확인

![alt text](image-10.png)

![alt text](image-11.png)

- Spec은 2로 수정이 반영이 되었다
- 하지만 Observed Partitions는 계속 6으로 수정이 되지 않았다
- conditions를 보면 `Ready=False (partition decrease not allowed)` 로 잘 표시되어있다

다음 시나리오를 위해 spec의 partitions를 6으로 다시 수정한다. Generation은 4가 된다.

#### 토픽이 실제로 잘 생성되고 있을까?

![alt text](image-12.png)

- 커맨드 설명 
    - `kubectl kcat —rm -it —restart=Never -n team-2 ~~`
    - Kafka Client 역할을 하는 임시 pod를 생성해서 토픽 정보를 요청한다.

- 왜 이렇게 해야하나?
    - 카프카 브로커는 K8s 내부의 Private Network에 존재한다. 외부에서는 접근이 불가능하기 때문에 내부 pod를 통해서만 접근할 수 있다.

---

## 시나리오 4: Spec의 config 값을 수정

> 목표: 수정된 Spec의 Config값과 동일하게 reconcile


#### config drift를 발생시키기
- spec의 retention 설정을 600000ms 으로 수정

![alt text](image-13.png)

#### describe로 Config 확인

![alt text](image-14.png)

![alt text](image-15.png)

#### 정말 kafka 브로커에 반영이 되었는지 확인

![alt text](image-16.png)

---

## 시나리오 5: 외부에서 토픽을 삭제하면 토픽 재생성하기

> 목적: 누가 토픽을 지워도 오퍼레이터가 desired state로 복원.

- 새로 알게된 점: 토픽이 삭제되면 토픽 안에 있는 데이터가 날라간다. 데이터가 날라가면 토픽을 재생성 하는 것이 의미가 없다. 때문에 실제 운영 상황에서는 삭제 권한을 애초에 주지 않음으로서 토픽의 삭제 가능성을 차단한다고 한다.

#### 외부에서 카프카 토픽 지우기, 브로커 상태 직접 확인해보기

![alt text](image-17.png)
- 토픽이 실제로 삭제되었다가 재생성 된 것을 확인할 수 있다.

#### 햇갈렸던 부분: CR의 Generation이 왜 그대로일까?

![alt text](image-18.png)

![alt text](image-19.png)

- 원칙: Spec 변경 시에만 generation이 증가한다
- CR도 재생성 되면서 generation이 1로 초기화되지 않을까?
    - 토픽이 실제로 삭제되고 생성되는 과정에서도 CR은 계속 살아있다.
- 컨트롤러가 토픽을 재생성하는 과정에서는 Spec을 건드리지 않는다. Status만 수정하기에 generation은 그대로이다.

---

## 시나리오 6: 브로커가 장애로 다운된 경우

> 목표: 연결에 실패하는 동안 `Ready=False(KafkaUnreachable)`로 Condition 표시, 30초 주기로 requeue. 연결 성공시 `Ready=True` 반영.

#### kafka statefulset의 replicas를 0으로 설정해서 브로커 내려버리기

- 커맨드: `kubectl scale statefulset kafka -n team-2 --replicas=0`

#### describe로 토픽 확인

![alt text](image-20.png)

![alt text](image-21.png)

- 브로커 접근에 실패하여 `Ready=False`, `KafkaUnreachable` 이 뜬다.

#### 다시 브로커 띄우기

- 커맨드: `kubectl scale statefulset kafka -n team-2 --replicas=1`
  
#### 30초 주기로 requeue해서 Reconcile 시도

![alt text](image-22.png)

![alt text](image-23.png)

- `Ready=True`로 desired state 회복 성공

## 시나리오 7: Finalizer 기반으로 토픽 삭제

> 목표: CR 삭제 요청시 Finalizer를 통해 실제 Kafka 토픽을 먼저 삭제한 뒤 CR을 삭제하기

#### CR 삭제 요청하기
![alt text](image-24.png)

#### 토픽이 실제로 삭제되었는지 확인하기
![alt text](image-26.png)

#### CR도 삭제되었는지 확인
![alt text](image-25.png)

---

## 아쉬웠던 부분

첫 계획은 K8s 클러스터 외부에서 운영중인 카프카 클러스터의 토픽을 관리하는 것이었다.
하지만 다른 네트워크에 둘 경우 수정해야 할 부분이 꽤 많아서, 시간 상 카프카도 K8s 클러스터 내부에 띄우게 되었다.

- 그럼에도:
카프카를 K8s 내부에서 운영하더라도 카프카의 토픽은 애플리케이션 내부의 영역이기 때문에 선언형 관리를 위해서는 커스텀 operator가 꼭 필요하다.
6주차까지 공부한 모든 내용을 다 활용해야하는 좋은 주제였다고 생각한다.