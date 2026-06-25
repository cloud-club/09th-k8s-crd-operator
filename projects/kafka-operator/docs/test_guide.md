# 로컬 테스트 가이드 — 패턴 B (operator in k8s → 외부 Kafka)

이 문서는 **operator를 kind 클러스터 안에 배포하고, Kafka는 호스트(Mac)에서 돌리는** 구성(패턴 B)을
로컬에서 재현하는 절차를 정리한다. "k8s 안의 operator가 클러스터 밖 Kafka에 요청을 보낼 수 있는가"를
실제로 검증하기 위한 셋업이다.

## 배경: 설정 주입 흐름

operator는 Kafka 주소를 `--kafka-bootstrap` 플래그로 받는다(`cmd/main.go`). 이 플래그에 값을 넣는 경로는
실행 환경에 따라 다르다.

| 환경 | 값 주입 경로 |
|---|---|
| 호스트 직접 실행 (`make run`) | CLI 인자: `go run ./cmd --kafka-bootstrap=localhost:9092` |
| k8s 배포 (`make deploy`) | ConfigMap → 환경변수 → 플래그 |

k8s 배포 시 값이 흐르는 체인:

```
ConfigMap(kafka-operator-config).bootstrap
  → env KAFKA_BOOTSTRAP
  → arg --kafka-bootstrap=$(KAFKA_BOOTSTRAP)
  → main.go flag.StringVar(&kafkaBootstrap, ...)
```

코드(`cmd/main.go`)는 두 환경에서 동일하다. **값만 바깥에서 주입**한다.

## 주의사항 2가지

### 1. advertised.listeners

Kafka 클라이언트는 부트스트랩 주소로 처음 접속한 뒤, 브로커가 알려주는 `advertised.listeners` 주소로 **재연결**한다. 따라서 advertised 주소가 파드에서 도달 가능해야 한다.

- 호스트 개발용(`hack/local/docker-compose.yaml`)은 `localhost`를 advertise → 호스트에서만 접속 가능.
- 파드 안에서 `localhost`는 파드 자기 자신이므로 연결이 깨진다.
- 패턴 B용(`hack/local/docker-compose.kind.yaml`)은 `host.docker.internal`을 advertise → 파드에서 도달 가능.

### 2. imagePullPolicy

`controller:latest` 이미지는 `:latest` 태그라 k8s 기본 `imagePullPolicy`가 `Always`다. 레지스트리에서 받으려다 실패(`ImagePullBackOff`)하므로, kind에 직접 로드한 로컬 이미지를 쓰려면
`imagePullPolicy: IfNotPresent`가 필요하다 (`config/manager/manager.yaml`에 반영됨).

---

## prerequisite

- Docker Desktop (Mac)
- `kind`, `kubectl`
- 이 저장소 (`projects/kafka-operator` 디렉토리에서 실행)

> 파드가 호스트에 도달하는 `host.docker.internal` 경로는 Docker Desktop(Mac/Windows) 기준.
> 리눅스 네이티브 도커에서는 다른 주소가 필요할 수도 있다.

---

## 절차

### 1. kind 클러스터 생성 / 컨텍스트 설정

```bash
kind create cluster --name kafka-op          # 이미 있으면 생략
kind export kubeconfig --name kafka-op       # kubectl 컨텍스트를 kind-kafka-op 로
kubectl config current-context               # kind-kafka-op 확인
```

### 2. 호스트에 Kafka 기동 (패턴 B용)

```bash
docker compose -f hack/local/docker-compose.kind.yaml up -d
# healthy 될 때까지 대기
```

`host.docker.internal:9092`을 advertise하는 단일 KRaft 브로커가 뜬다.

### 3. (선택) 파드 → 호스트 도달성 사전 검증

operator를 올리기 전에 네트워크 경로만 먼저 확인하고 싶다면:

```bash
kubectl run nettest --image=busybox:1.36 --restart=Never --command -- sleep 600
kubectl wait --for=condition=Ready pod/nettest --timeout=60s

# DNS 해석 (host.docker.internal → 192.168.65.254)
kubectl exec nettest -- nslookup host.docker.internal

# TCP 연결
kubectl exec nettest -- nc -z -w 3 host.docker.internal 9092 && echo OK

kubectl delete pod nettest
```

### 4. operator 이미지 빌드 → kind 로드

kind는 로컬 도커 이미지를 자동으로 보지 못하므로 `kind load`로 클러스터에 밀어넣어야 한다.

```bash
make docker-build IMG=controller:latest
kind load docker-image controller:latest --name kafka-op
```

### 5. CRD 설치 + operator 배포

```bash
make install                       # CRD 등록
make deploy IMG=controller:latest  # operator + ConfigMap + RBAC 등 배포
```

ConfigMap(`config/manager/configmap.yaml`)의 `bootstrap` 기본값이 `host.docker.internal:9092`라
별도 수정 없이 패턴 B로 연결된다.

### 6. operator 기동 확인

```bash
kubectl get pods -n kafka-operator-system

# real Kafka client 로 떴는지 (fake 말고) 로그 확인
kubectl logs -n kafka-operator-system -l control-plane=controller-manager --tail=30 | grep -i kafka
# → "Using real Kafka client {"bootstrap": "host.docker.internal:9092"}"
```

### 7. KafkaTopic 적용 → 토픽 생성 검증

operator는 `team-2` 네임스페이스를 watch한다(`--watch-namespace` 기본값). CR도 거기에 적용한다.

```bash
kubectl create namespace team-2          # 없으면 생성
kubectl apply -n team-2 -f config/samples/kafka_v1alpha1_kafkatopic.yaml

# CR 상태 (READY=True 기대)
kubectl get kafkatopic -n team-2 -o wide

# 호스트 Kafka 에 실제로 토픽이 생겼는지 브로커에 직접 질의
docker exec kafka-kind /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --list
docker exec kafka-kind /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 \
  --describe --topic test-topic
```

기대 결과: `test-topic` (PartitionCount=3, ReplicationFactor=1) 존재.

---

## 검증 완료 시 체크리스트

| 항목 | 기대 |
|---|---|
| 파드 → `host.docker.internal` DNS | `192.168.65.254`로 해석 |
| 파드 → `host.docker.internal:9092` TCP | 연결 성공 |
| operator 로그 | `Using real Kafka client ... host.docker.internal:9092` |
| `kubectl get kafkatopic -n team-2` | `READY=True` |
| 호스트 Kafka 토픽 목록 | `test-topic` 존재 |

---

## 정리 (실험 종료)

```bash
kubectl delete -n team-2 -f config/samples/kafka_v1alpha1_kafkatopic.yaml
make undeploy
docker compose -f hack/local/docker-compose.kind.yaml down -v
# 클러스터까지 제거하려면:
# kind delete cluster --name kafka-op
```

---

## 부록: ConfigMap 값 변경

브로커 주소를 바꿀 때는 코드/이미지 재빌드 없이 ConfigMap만 수정한다. 단 env 주입 방식이라
**파드 재시작이 필요**하다.

```bash
kubectl edit configmap kafka-operator-config -n kafka-operator-system   # bootstrap 값 수정
kubectl rollout restart deploy/kafka-operator-controller-manager -n kafka-operator-system
```

## 부록: 오늘 작업으로 추가/변경된 파일

| 파일 | 내용 |
|---|---|
| `config/manager/configmap.yaml` | 신규 — `bootstrap` 값 보관 |
| `config/manager/kustomization.yaml` | configmap.yaml 등록 (+ image 태그) |
| `config/manager/manager.yaml` | `--kafka-bootstrap=$(KAFKA_BOOTSTRAP)` arg, ConfigMap 참조 env, `imagePullPolicy: IfNotPresent` |
| `hack/local/docker-compose.kind.yaml` | 신규 — 패턴 B용 Kafka (`host.docker.internal` advertise) |

`cmd/main.go`는 변경하지 않았다 — 설정 외부화의 의도대로 코드는 그대로 둔다.
