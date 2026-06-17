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

// AdminClient is the real kafka.Client backed by franz-go's kadm admin API.
// It is safe for concurrent use (kgo.Client maintains an internal connection
// pool). Construct it with NewClient and Close it on shutdown.
type AdminClient struct {
	cl  *kgo.Client
	adm *kadm.Client
}

// Compile-time check that *AdminClient satisfies kafka.Client.
var _ Client = (*AdminClient)(nil)

// Option configures an AdminClient.
type Option func(*config)

type config struct {
	dialTimeout time.Duration
	clientID    string
}

// WithDialTimeout sets the broker dial timeout. Default 10s.
func WithDialTimeout(d time.Duration) Option {
	return func(c *config) { c.dialTimeout = d }
}

// WithClientID sets the Kafka client.id reported to the broker.
func WithClientID(id string) Option {
	return func(c *config) { c.clientID = id }
}

// NewClient dials the given bootstrap brokers and returns an admin client.
// brokers come from operator flags/env (single external Kafka cluster, see
// docs/kafka-client-interface.md §9). It does not block on connectivity;
// per-call errors surface as ErrKafkaUnreachable when the broker is down.
func NewClient(brokers []string, opts ...Option) (*AdminClient, error) {
	cfg := config{dialTimeout: 10 * time.Second, clientID: "kafka-topic-operator"}
	for _, opt := range opts {
		opt(&cfg)
	}
	if len(brokers) == 0 {
		return nil, errors.New("kafka: no bootstrap brokers provided")
	}

	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.DialTimeout(cfg.dialTimeout),
		kgo.ClientID(cfg.clientID),
	)
	if err != nil {
		return nil, fmt.Errorf("kafka: new client: %w", err)
	}
	return &AdminClient{cl: cl, adm: kadm.NewClient(cl)}, nil
}

// Close releases the underlying connections.
func (c *AdminClient) Close() { c.cl.Close() }

// DescribeTopic returns the observed state of a topic, or ErrTopicNotFound.
// Config holds the full set of effective config keys reported by Kafka; the
// controller compares only the keys it manages.
func (c *AdminClient) DescribeTopic(ctx context.Context, name string) (*TopicInfo, error) {
	td, err := c.adm.ListTopics(ctx, name)
	if err != nil {
		return nil, mapErr(err)
	}
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
	// Replication factor is the replica count of any partition (uniform per topic).
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

// describeConfig returns the effective config map for a topic.
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

// CreateTopic creates a topic, or returns ErrTopicAlreadyExists.
func (c *AdminClient) CreateTopic(ctx context.Context, spec TopicSpec) error {
	_, err := c.adm.CreateTopic(ctx, spec.Partitions, spec.ReplicationFactor, toPtrMap(spec.Config), spec.Name)
	if errors.Is(err, kerr.TopicAlreadyExists) {
		return ErrTopicAlreadyExists
	}
	return mapErr(err)
}

// DeleteTopic deletes a topic. Returns ErrTopicNotFound when it does not exist
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

// UpdateConfig overrides only the supplied keys (incremental AlterConfigs SET);
// keys absent from the map are left untouched. Returns ErrTopicNotFound when
// the topic does not exist.
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

// AddPartitions raises the total partition count to total. Equal is a noop;
// lower returns *PartitionDecreaseError. Returns ErrTopicNotFound when missing.
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

// mapErr translates franz-go errors into the package's sentinels. A genuine
// Kafka protocol error (*kerr.Error) is surfaced as-is so callers can branch
// on specific codes; anything else (dial, timeout, context, closed client) is
// treated as broker-unreachable.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var ke *kerr.Error
	if errors.As(err, &ke) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrKafkaUnreachable, err)
}

// toPtrMap converts a config map to the *string form kadm expects.
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
