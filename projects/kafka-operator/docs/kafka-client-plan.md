# Kafka Client (담당자 1) — 1주차 계획서

> 담당자 1의 작업 범위와 순서를 정리한 문서. source of truth 는 `internal/kafka/interface.go`,
> 설계 rationale 는 `kafka-client-interface.md` 이며, 이 문서는 **무엇을 어떤 순서로 만들지**에 집중한다.

## 0. 범위 / 경계

| 구분 | 내용 |
|---|---|
| 담당 파일 | `internal/kafka/client.go` (+ 통합 테스트, 로컬 Kafka 환경) |
| 담당 영역 | Kafka Admin API 호출 + Kafka 도메인 로직 |
| 비담당 | Reconcile / Status / Finalizer / CRD (담당자 2) |
| 계약 | `internal/kafka/interface.go` 의 `kafka.Client` 인터페이스 (이미 확정됨) |
| 멱등성/재시도 | **호출자(controller) 책임.** 클라이언트는 typed error 로 상태만 알린다 |

`kafka.Client` 인터페이스, `TopicInfo` / `TopicSpec`, sentinel 에러(`ErrTopicNotFound`,
`ErrTopicAlreadyExists`, `ErrKafkaUnreachable`, `ErrPartitionDecrease`)와 `PartitionDecreaseError`
는 **담당자 2가 스캐폴딩 단계에서 이미 정의**해 두었다. 따라서 `client.go` 에서는 이들을 **재정의하지 않고
같은 `kafka` 패키지의 심볼을 그대로 사용**해 구현체만 채운다.

```go
type Client interface {
    DescribeTopic(ctx context.Context, name string) (*TopicInfo, error)
    CreateTopic(ctx context.Context, spec TopicSpec) error
    DeleteTopic(ctx context.Context, name string) error
    UpdateConfig(ctx context.Context, name string, config map[string]string) error
    AddPartitions(ctx context.Context, name string, total int32) error
}
```

## 1. 라이브러리 선택: franz-go (`twmb/franz-go`)

| 기준 | 판단 |
|---|---|
| 순수 Go (CGO X) | ✅ distroless 컨테이너 그대로 구동, librdkafka 시스템 의존 없음 |
| admin 추상화 | `pkg/kadm` 가 인터페이스 5개 메서드와 거의 1:1 매핑 |
| incremental AlterConfigs | `AlterTopicConfigs` 로 명시 key 만 override (계약 §5 충족) |
| DescribeConfigs 전체 반환 | `DescribeTopicConfigs` 로 effective config 전체 조회 가능 |
| 파티션 증가 | `UpdatePartitions(ctx, total, name)` |
| 최신 KIP 지원 | franz-go 가 생태계에서 가장 적극적 |

> 후보였던 `segmentio/kafka-go` 는 admin API 가 저수준(프로토콜 메시지 직접 조립)이라 incremental
> AlterConfigs / 에러→sentinel 매핑 코드가 길어지고, `confluent-kafka-go` 는 CGO+librdkafka 의존으로
> distroless 배포가 까다로워 제외.

사용 패키지:
- `github.com/twmb/franz-go/pkg/kgo` — 기반 클라이언트 (커넥션 풀)
- `github.com/twmb/franz-go/pkg/kadm` — admin 작업
- `github.com/twmb/franz-go/pkg/kerr` — Kafka 에러 코드 → sentinel 매핑용

## 2. 브로커 접속 정보 주입 (계약 §9 오픈 이슈)

1주차 잠정 결정 **(A) flag/env** 를 따른다. 단일 외부 Kafka 클러스터를 가정하고
`NewClient` 가 bootstrap 주소를 인자로 받는다. SASL/TLS 가 필요해지면 `Option` 으로 확장한다.

```go
// internal/kafka/client.go
func NewClient(brokers []string, opts ...Option) (*AdminClient, error)
```

`cmd/main.go:198` 의 `fake.New()` 를 추후 `kafka.NewClient(strings.Split(bootstrapServers, ","))`
로 교체하면 통합된다. (담당자 2와 통합 시점에 조율)

## 3. 작업 순서 (1주차)

| # | 작업 | 산출물 | 검증 |
|---|---|---|---|
| 1 | 로컬 Kafka 환경 | `hack/local/docker-compose.yaml` (KRaft 단일 브로커) | `docker compose up`, 9092 LISTEN |
| 2 | 접속 테스트 | `hack/kafka-smoke/main.go` | 브로커 메타데이터/버전 출력 |
| 3 | 의존성 추가 | `go.mod` / `go.sum` 에 franz-go | `go build ./...` 통과 |
| 4 | 클라이언트 구현 | `internal/kafka/client.go` | `var _ kafka.Client = (*AdminClient)(nil)` |
| 5 | 통합 테스트 | `internal/kafka/client_integration_test.go` (`//go:build integration`) | 실 Kafka 대상 green |

### 메서드별 구현 메모 (계약 §5 기준)

- **DescribeTopic** — `kadm.ListTopics`/`DescribeTopicConfigs`. 없으면 `ErrTopicNotFound`.
  `Config` 는 effective config 전체를 채워 반환(드리프트 비교는 controller 가 spec key 만 본다).
- **CreateTopic** — `kadm.CreateTopic`. 이미 있으면 `ErrTopicAlreadyExists`
  (`kerr.TopicAlreadyExists` 매핑).
- **DeleteTopic** — `kadm.DeleteTopics`. 없으면 `ErrTopicNotFound`
  (호출자가 nil 로 변환해 finalizer 멱등 처리).
- **UpdateConfig** — `kadm.AlterTopicConfigs` (**incremental SET**). 전달된 key 만 override.
  없으면 `ErrTopicNotFound`.
- **AddPartitions** — `kadm.UpdatePartitions(ctx, total, name)`. `total == current` → noop(nil),
  `total < current` → `*PartitionDecreaseError{Current, Desired}`. 감소 판단을 위해 현재 파티션 수를
  먼저 조회한다.
- **공통** — 브로커 연결 실패(네트워크/타임아웃/인증)는 `ErrKafkaUnreachable` 로 wrap.
  `errors.Is` 분기가 가능하도록 sentinel 을 Unwrap 체인에 유지한다.

## 4. 테스트 전략

- 단위 검증이 아닌 **실 Kafka(docker-compose) 통합 테스트** 로 happy path + 에러 케이스를 확인한다.
- CI 에서 Kafka 없이 도는 단위 테스트와 섞이지 않도록 `//go:build integration` 태그로 분리.
  (`go test -tags=integration ./internal/kafka/...`)
- 시나리오: `Create → Describe → UpdateConfig → AddPartitions(증가) → AddPartitions(감소=에러) → Delete`
  + 중복 생성/없는 토픽 describe·delete 에러 케이스.

## 4-1. 통합 가이드 (담당자 2용 — `cmd/main.go` 교체)

> 역할분담상 mock → 실제 client 교체는 담당자 2의 1주차 5번 통합 태스크다. 아래는 담당자 1이
> 로컬에서 직접 검증(kind + docker-compose Kafka)한 패치 내용으로, 그대로 적용하면 동작한다.
> 검증 결과: CR 적용 → `Ready=True`(reason `TopicSynced`) → 실제 Kafka 토픽 + config 생성 확인.

`cmd/main.go` 의 `kafkaClient := fake.New()` 한 줄을 아래로 교체한다:

```go
// import 추가: "strings" 및 internal/kafka 패키지
var kafkaClient kafka.Client
if kafkaBootstrap == "" {
    kafkaClient = fake.New()
    setupLog.Info("Using in-memory fake Kafka client", "warning", "topics will not survive restart")
} else {
    realClient, err := kafka.NewClient(strings.Split(kafkaBootstrap, ","))
    if err != nil {
        setupLog.Error(err, "Failed to create Kafka client")
        os.Exit(1)
    }
    defer realClient.Close()
    kafkaClient = realClient
    setupLog.Info("Using real Kafka client", "bootstrap", kafkaBootstrap)
}
```

플래그 추가(기존 `watch-namespace` flag 근처):

```go
flag.StringVar(&kafkaBootstrap, "kafka-bootstrap", "localhost:9092",
    "Comma-separated Kafka bootstrap servers. Set to empty to use the in-memory fake client.")
```

`-kafka-bootstrap=""` 로 비우면 fake 로 떨어지므로 담당자 2의 envtest/fake 테스트는 영향 없음.

검증 절차:

```bash
docker compose -f hack/local/docker-compose.yaml up -d
kubectl apply -f config/crd/bases/kafka.study.dev_kafkatopics.yaml
kubectl create namespace team-2
go run ./cmd -kafka-bootstrap=localhost:9092 -metrics-bind-address=0 -health-probe-bind-address=:8099
# 다른 셸: kubectl apply 로 KafkaTopic CR → kubectl get kafkatopic -n team-2 에서 READY=True 확인
```

## 5. 2주차 예고 (참고)

drift 비교 헬퍼(spec.config vs effective config diff)와 파티션 처리 보강. 인터페이스에 `AddPartitions`
가 이미 포함되어 있어 시그니처 break 없이 진행 가능.

## 6. 변경 이력

- 2026-06-18: 초안. franz-go/kadm 채택, 1주차 작업 순서/계약 매핑 정리.
