# KafkaTopic Operator — 조회 / 시연 명령어

가비아 클러스터 `team-2` 네임스페이스에 배포된 KafkaTopic Operator를 조회·테스트하는 명령어 모음.

> **이름 2개 주의**: KafkaTopic은 이름이 둘이다.
> - **CR 이름** (`metadata.name`) — `kubectl get kafkatopic <여기>`
> - **토픽 이름** (`spec.topicName`) — 실제 Kafka에 생기는 토픽 (`kafka-topics --list`)
>
> 예: 샘플 CR은 CR 이름 `kafkatopic-sample`, 실제 토픽 `test-topic`.

---

## 0. 사전 설정 (한 번)

```bash
kubectl config current-context                              # cluster-260606003819 인지 확인
kubectl config set-context --current --namespace=team-2     # 기본 ns를 team-2로
```

---

## 1. 조회 — 지금 뭐가 떠 있나

```bash
# 오퍼레이터 + Kafka 파드
kubectl get pod -n team-2
#   kafka-0                              → Kafka 브로커 (in-cluster)
#   kafka-operator-controller-manager-* → 우리가 만든 오퍼레이터

# KafkaTopic CR 목록 (NAME=CR 이름, TOPIC=실제 토픽 이름)
kubectl get kafkatopic -n team-2

# 특정 CR 상태 (conditions)
kubectl get kafkatopic <CR이름> -n team-2 -o jsonpath='{range .status.conditions[*]}{.type}={.status}({.reason}) {.message}{"\n"}{end}'
kubectl describe kafkatopic <CR이름> -n team-2

# spec.topicName 만 확인
kubectl get kafkatopic <CR이름> -n team-2 -o jsonpath='{.spec.topicName}{"\n"}'

# 오퍼레이터 로그 (실시간)
kubectl logs -n team-2 -l control-plane=controller-manager -f
```

---

## 2. 시연 — 전체 기능 흐름

기존 `kafkatopic-sample`을 써도 되고, 아래처럼 새 CR을 만들어도 동작은 동일하다.

### (a) 생성
```bash
kubectl apply -n team-2 -f - <<'EOF'
apiVersion: kafka.study.dev/v1alpha1
kind: KafkaTopic
metadata:
  name: demo
  namespace: team-2
spec:
  topicName: demo-topic
  partitions: 3
  replicationFactor: 1
  config:
    retention.ms: "604800000"
    cleanup.policy: "delete"
EOF
sleep 5; kubectl get kafkatopic demo -n team-2          # READY=True
```

### (b) 파티션 증가 (3 → 6)
```bash
kubectl patch kafkatopic demo -n team-2 --type=merge -p '{"spec":{"partitions":6}}'
sleep 4; kubectl get kafkatopic demo -n team-2          # PARTITIONS 6
```

### (c) config drift 자동 교정
```bash
kubectl patch kafkatopic demo -n team-2 --type=merge -p '{"spec":{"config":{"retention.ms":"600000"}}}'
sleep 4; kubectl get kafkatopic demo -n team-2 \
  -o jsonpath='{range .status.conditions[?(@.type=="ConfigDrifted")]}{.status}({.reason})\n{end}'
```

### (d) 파티션 감소 시도 → 거부 (6 → 2)
```bash
kubectl patch kafkatopic demo -n team-2 --type=merge -p '{"spec":{"partitions":2}}'
sleep 4; kubectl get kafkatopic demo -n team-2 \
  -o jsonpath='{range .status.conditions[?(@.type=="Ready")]}{.status}({.reason})\n{end}'
#   → False(PartitionDecreaseNotAllowed)  — Kafka는 6 유지(데이터 보호)
```

### (e) Finalizer 삭제 (CR 지우면 Kafka 토픽도 정리)
```bash
# 감소 테스트 후 Ready=False면 먼저 복구
kubectl patch kafkatopic demo -n team-2 --type=merge -p '{"spec":{"partitions":6}}'

kubectl delete kafkatopic demo -n team-2                # finalizer가 Kafka 토픽 삭제 후 CR 제거
```

---

## 3. Kafka에 실제 토픽 확인 (직접)

```bash
# 토픽 목록
kubectl run kcat --rm -it --restart=Never -n team-2 --image=apache/kafka:3.9.0 -- \
  /opt/kafka/bin/kafka-topics.sh --bootstrap-server kafka.team-2.svc.cluster.local:9092 --list

# 특정 토픽 상세 (파티션 수 / config)
kubectl run kcat --rm -it --restart=Never -n team-2 --image=apache/kafka:3.9.0 -- \
  /opt/kafka/bin/kafka-topics.sh --bootstrap-server kafka.team-2.svc.cluster.local:9092 --describe --topic demo-topic
```

---

## 4. 정리 (데모 후)

> ⚠️ **`make undeploy` 금지** — 클러스터 전역 리소스인 CRD(`kafkatopics.kafka.study.dev`)까지 삭제된다 (공유 클러스터 규칙 위반).

```bash
kubectl delete kafkatopic --all -n team-2                                  # CR 먼저 (finalizer로 토픽 정리)
kubectl delete deploy,svc,cm,sa -l app.kubernetes.io/name=kafka-operator -n team-2
kubectl delete -f hack/gabia/kafka.yaml                                    # in-cluster Kafka
# CRD는 남겨둔다 (전역 리소스 임의 삭제 금지)
```

---

## 참고: 상태 필드 의미

| Condition | True 의미 | 주요 reason |
|---|---|---|
| `Ready` | 토픽이 spec과 동기화됨 | `TopicSynced`, `KafkaUnreachable`, `PartitionDecreaseNotAllowed` |
| `ConfigDrifted` | spec.config와 실제 Kafka config 불일치 감지 | `DriftDetected`, `InSync` |

- `observedPartitions` — Kafka에서 관찰된 실제 파티션 수
- `observedGeneration` — 처리 완료된 spec 버전
