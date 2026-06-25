/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kafka

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

// franz-go의 kadm admin API 클라이언트를 담는 구조체
// 생성자 NewClient에서 생성하고, 종료시 close로 커넥션 반납
type AdminClient struct {
	cl  *kgo.Client  // 기본 kafka 클라이언트
	adm *kadm.Client // cl을 감싸는 wrapper
}

// kafka.Client 인터페이스를 잘 구현했는지 Compile-time에 검사
// 메서드 구현 안되어있는 부분 있으면 에러남
var _ Client = (*AdminClient)(nil)

// *config를 인자로 받는 함수 타입 Option
type Option func(*config)

type config struct {
	dialTimeout time.Duration
	clientID    string
}

// 브로커 연결 타임아웃을 설정하는 Option을 반환
func WithDialTimeout(d time.Duration) Option {
	return func(c *config) { c.dialTimeout = d }
}

// 브로커가 식별할 client.id를 설정하는 Option 반환
func WithClientID(id string) Option {
	return func(c *config) { c.clientID = id }
}

// NewClient는 bootstrap broker와 연결된 admin client를 생성
func NewClient(brokers []string, opts ...Option) (*AdminClient, error) {
	// 1. 기본 설정값(타임아웃 10초, 클라이언트 ID 지정)으로 config 구조체를 초기화
	cfg := config{dialTimeout: 10 * time.Second, clientID: "kafka-topic-operator"}
	// 2. 외부에서 주입된 가변 옵션 함수(opts)들을 순회하며 기본 설정을 덮어씁니다.
	for _, opt := range opts {
		opt(&cfg)
	}
	// 3. 연결할 카프카 브로커 주소(bootstrap brokers)가 없으면 에러를 반환
	if len(brokers) == 0 {
		return nil, errors.New("kafka: no bootstrap brokers provided")
	}

	// 4. franz-go 라이브러리를 사용해 카프카 코어 클라이언트를 생성
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.DialTimeout(cfg.dialTimeout),
		kgo.ClientID(cfg.clientID),
	)
	if err != nil {
		return nil, fmt.Errorf("kafka: new client: %w", err)
	}
	// 5. 생성된 코어 클라이언트(cl)를 기반으로 admin 클라이언트(adm)를 함께 묶어서 반환
	return &AdminClient{cl: cl, adm: kadm.NewClient(cl)}, nil
}

// 클라이언트가 사용 중이던 내부 네트워크 커넥션 풀을 안전하게 닫는다.
func (c *AdminClient) Close() { c.cl.Close() }

// 토픽의 observed state를 반환
// 컨트롤러가 desired state랑 비교함
func (c *AdminClient) DescribeTopic(ctx context.Context, name string) (*TopicInfo, error) {
	// 1. 카프카 브로커에 해당 이름의 토픽 정보를 요청
	td, err := c.adm.ListTopics(ctx, name)
	if err != nil {
		return nil, mapErr(err) // 네트워크 에러 등을 도메인 에러로 변환
	}
	// 2. 반환된 결과 맵에서 우리가 요청한 토픽 데이터가 있는지 확인
	detail, ok := td[name]
	if !ok || errors.Is(detail.Err, kerr.UnknownTopicOrPartition) {
		return nil, ErrTopicNotFound
	}
	if detail.Err != nil {
		return nil, mapErr(detail.Err)
	}

	info := &TopicInfo{
		Name:       name,
		Partitions: int32(len(detail.Partitions)),
	}

	// 특정 토픽의 파티션 replica count는 동일하기에, 한 파티션만 확인해도 된다.
	for _, p := range detail.Partitions {
		info.ReplicationFactor = int16(len(p.Replicas))
		break
	}

	cfg, err := c.describeConfig(ctx, name)
	if err != nil {
		return nil, err
	}
	info.Config = cfg
	return info, nil
}

// 토픽의 config map을 반환하는 함수
func (c *AdminClient) describeConfig(ctx context.Context, name string) (map[string]string, error) {
	rcs, err := c.adm.DescribeTopicConfigs(ctx, name)
	if err != nil {
		return nil, mapErr(err)
	}
	rc, err := rcs.On(name, nil)
	if errors.Is(err, kerr.UnknownTopicOrPartition) || errors.Is(rc.Err, kerr.UnknownTopicOrPartition) {
		return nil, ErrTopicNotFound
	}
	if err != nil {
		return nil, mapErr(err)
	}
	if rc.Err != nil {
		return nil, mapErr(rc.Err)
	}
	out := make(map[string]string, len(rc.Configs))
	for _, kv := range rc.Configs {
		out[kv.Key] = kv.MaybeValue()
	}
	return out, nil
}

// 토픽을 생성하는 함수
func (c *AdminClient) CreateTopic(ctx context.Context, spec TopicSpec) error {
	_, err := c.adm.CreateTopic(ctx, spec.Partitions, spec.ReplicationFactor, toPtrMap(spec.Config), spec.Name)
	if errors.Is(err, kerr.TopicAlreadyExists) {
		return ErrTopicAlreadyExists
	}
	return mapErr(err)
}

// 토픽을 삭제하는 함수
// (the controller converts this to nil for finalizer idempotency).
func (c *AdminClient) DeleteTopic(ctx context.Context, name string) error {
	resp, err := c.adm.DeleteTopic(ctx, name)
	if err == nil {
		err = resp.Err
	}
	if errors.Is(err, kerr.UnknownTopicOrPartition) {
		return ErrTopicNotFound
	}
	return mapErr(err)
}

// UpdateConfig가 지금은 주어진 키만 업데이트한다.(incremental AlterConfigs SET),
// 만약 사용자가 spec.Config에서 키를 제거했을 때 Kafka의 기존 override를 default로 되돌리고 싶다면, 그건 현재 인터페이스로 표현이 불가능
func (c *AdminClient) UpdateConfig(ctx context.Context, name string, cfg map[string]string) error {
	if len(cfg) == 0 {
		return nil
	}
	alters := make([]kadm.AlterConfig, 0, len(cfg))
	for k, v := range cfg {
		v := v
		alters = append(alters, kadm.AlterConfig{Op: kadm.SetConfig, Name: k, Value: &v})
	}
	resps, err := c.adm.AlterTopicConfigs(ctx, alters, name)
	if err != nil {
		return mapErr(err)
	}
	resp, err := resps.On(name, nil)
	if errors.Is(err, kerr.UnknownTopicOrPartition) || errors.Is(resp.Err, kerr.UnknownTopicOrPartition) {
		return ErrTopicNotFound
	}
	if err != nil {
		return mapErr(err)
	}
	return mapErr(resp.Err)
}

// 파티션이 total개가 되도록 하는 함수. 이미 total개면 noop
// total이 현재 파티션 수보다 작으면 *PartitionDecreaseError
// 존재하지 않는 토픽이면 ErrTopicNotFound
func (c *AdminClient) AddPartitions(ctx context.Context, name string, total int32) error {
	td, err := c.adm.ListTopics(ctx, name)
	if err != nil {
		return mapErr(err)
	}
	detail, ok := td[name]
	if !ok || errors.Is(detail.Err, kerr.UnknownTopicOrPartition) {
		return ErrTopicNotFound
	}
	if detail.Err != nil {
		return mapErr(detail.Err)
	}

	current := int32(len(detail.Partitions))
	switch {
	case total == current:
		return nil
	case total < current:
		return &PartitionDecreaseError{Current: current, Desired: total}
	}

	resps, err := c.adm.UpdatePartitions(ctx, int(total), name)
	if err != nil {
		return mapErr(err)
	}
	resp, err := resps.On(name, nil)
	if errors.Is(err, kerr.UnknownTopicOrPartition) || errors.Is(resp.Err, kerr.UnknownTopicOrPartition) {
		return ErrTopicNotFound
	}
	if err != nil {
		return mapErr(err)
	}
	return mapErr(resp.Err)
}

// franz-go 에러를 이 패키지 내부에서 다루는 표준 센티넬 에러로 매핑하는 헬퍼 함수
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	// 만약 에러의 원인이 순수 카프카 프로토콜 에러(*kerr.Error)라면,
	// 호출자가 내부 상세 오류 코드를 직접 분기 처리할 수 있도록 변환 없이 그대로 반환
	var ke *kerr.Error
	if errors.As(err, &ke) {
		return err
	}
	// 그 외의 에러(네트워크 단절, 타임아웃, 인증 실패, 클라이언트 닫힘 등)
	// 전부 상위 레이어에서 재시도(Requeue)할 수 있도록 ErrKafkaUnreachable 에러로 감싸서 반환
	return fmt.Errorf("%w: %v", ErrKafkaUnreachable, err)
}

// map[string]string 타입을 franz-go admin 라이브러리가 요구하는 map[string]*string 타입으로 변환
// 카프카 설정 변경 시, '설정값 비우기(null)'와 '빈 문자열 입력("")'을 명확히 구분하기 위해 포인터 맵을 사용
func toPtrMap(in map[string]string) map[string]*string {
	if in == nil {
		return nil
	}
	out := make(map[string]*string, len(in))
	for k, v := range in {
		v := v
		out[k] = &v
	}
	return out
}
