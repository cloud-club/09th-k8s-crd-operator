//go:build integration

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

// Integration tests for the real AdminClient against a live Kafka broker.
// They are excluded from normal `go test` by the `integration` build tag.
//
//	docker compose -f hack/local/docker-compose.yaml up -d
//	go test -tags=integration ./internal/kafka/...
//
// Override the broker with KAFKA_BOOTSTRAP=host:9092.
package kafka

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func bootstrap() []string {
	if v := os.Getenv("KAFKA_BOOTSTRAP"); v != "" {
		return []string{v}
	}
	return []string{"localhost:9092"}
}

func newTestClient(t *testing.T) *AdminClient {
	t.Helper()
	c, err := NewClient(bootstrap())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

// uniqueName keeps parallel/repeat runs from colliding on topic names.
func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// TestLifecycle walks the full happy path:
// Create → Describe → UpdateConfig → AddPartitions(increase) → Delete.
func TestLifecycle(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	name := uniqueName("op-it-lifecycle")
	t.Cleanup(func() { _ = c.DeleteTopic(context.Background(), name) })

	// Create
	spec := TopicSpec{
		Name:              name,
		Partitions:        2,
		ReplicationFactor: 1,
		Config:            map[string]string{"retention.ms": "600000"},
	}
	if err := c.CreateTopic(ctx, spec); err != nil {
		t.Fatalf("CreateTopic: %v", err)
	}

	// Describe reflects the created shape.
	info, err := c.DescribeTopic(ctx, name)
	if err != nil {
		t.Fatalf("DescribeTopic: %v", err)
	}
	if info.Partitions != 2 {
		t.Errorf("partitions = %d, want 2", info.Partitions)
	}
	if info.ReplicationFactor != 1 {
		t.Errorf("replicationFactor = %d, want 1", info.ReplicationFactor)
	}
	if got := info.Config["retention.ms"]; got != "600000" {
		t.Errorf("retention.ms = %q, want 600000", got)
	}

	// UpdateConfig overrides only the supplied key.
	if err := c.UpdateConfig(ctx, name, map[string]string{"retention.ms": "1200000"}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	info, err = c.DescribeTopic(ctx, name)
	if err != nil {
		t.Fatalf("DescribeTopic after update: %v", err)
	}
	if got := info.Config["retention.ms"]; got != "1200000" {
		t.Errorf("retention.ms after update = %q, want 1200000", got)
	}

	// AddPartitions: equal is a noop, increase raises the count.
	if err := c.AddPartitions(ctx, name, 2); err != nil {
		t.Errorf("AddPartitions(equal) should be noop, got %v", err)
	}
	if err := c.AddPartitions(ctx, name, 4); err != nil {
		t.Fatalf("AddPartitions(increase): %v", err)
	}
	info, err = c.DescribeTopic(ctx, name)
	if err != nil {
		t.Fatalf("DescribeTopic after AddPartitions: %v", err)
	}
	if info.Partitions != 4 {
		t.Errorf("partitions after increase = %d, want 4", info.Partitions)
	}

	// Delete, then Describe reports not found.
	if err := c.DeleteTopic(ctx, name); err != nil {
		t.Fatalf("DeleteTopic: %v", err)
	}
	if _, err := c.DescribeTopic(ctx, name); !errors.Is(err, ErrTopicNotFound) {
		t.Errorf("DescribeTopic after delete = %v, want ErrTopicNotFound", err)
	}
}

// TestPartitionDecrease verifies the typed error on a partition decrease.
func TestPartitionDecrease(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	name := uniqueName("op-it-decrease")
	t.Cleanup(func() { _ = c.DeleteTopic(context.Background(), name) })

	if err := c.CreateTopic(ctx, TopicSpec{Name: name, Partitions: 3, ReplicationFactor: 1}); err != nil {
		t.Fatalf("CreateTopic: %v", err)
	}

	err := c.AddPartitions(ctx, name, 1)
	if !errors.Is(err, ErrPartitionDecrease) {
		t.Fatalf("AddPartitions(decrease) = %v, want ErrPartitionDecrease", err)
	}
	var pe *PartitionDecreaseError
	if !errors.As(err, &pe) {
		t.Fatalf("error is not *PartitionDecreaseError: %v", err)
	}
	if pe.Current != 3 || pe.Desired != 1 {
		t.Errorf("PartitionDecreaseError = {Current:%d Desired:%d}, want {3 1}", pe.Current, pe.Desired)
	}
}

// TestErrorCases covers the sentinel errors for missing/duplicate topics.
func TestErrorCases(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	missing := uniqueName("op-it-missing")

	if _, err := c.DescribeTopic(ctx, missing); !errors.Is(err, ErrTopicNotFound) {
		t.Errorf("DescribeTopic(missing) = %v, want ErrTopicNotFound", err)
	}
	if err := c.DeleteTopic(ctx, missing); !errors.Is(err, ErrTopicNotFound) {
		t.Errorf("DeleteTopic(missing) = %v, want ErrTopicNotFound", err)
	}
	if err := c.UpdateConfig(ctx, missing, map[string]string{"retention.ms": "1000"}); !errors.Is(err, ErrTopicNotFound) {
		t.Errorf("UpdateConfig(missing) = %v, want ErrTopicNotFound", err)
	}
	if err := c.AddPartitions(ctx, missing, 5); !errors.Is(err, ErrTopicNotFound) {
		t.Errorf("AddPartitions(missing) = %v, want ErrTopicNotFound", err)
	}

	// Duplicate create.
	name := uniqueName("op-it-dup")
	t.Cleanup(func() { _ = c.DeleteTopic(context.Background(), name) })
	if err := c.CreateTopic(ctx, TopicSpec{Name: name, Partitions: 1, ReplicationFactor: 1}); err != nil {
		t.Fatalf("CreateTopic: %v", err)
	}
	if err := c.CreateTopic(ctx, TopicSpec{Name: name, Partitions: 1, ReplicationFactor: 1}); !errors.Is(err, ErrTopicAlreadyExists) {
		t.Errorf("CreateTopic(dup) = %v, want ErrTopicAlreadyExists", err)
	}
}

// TestKafkaUnreachable verifies the unreachable sentinel against a dead port.
func TestKafkaUnreachable(t *testing.T) {
	c, err := NewClient([]string{"127.0.0.1:1"}, WithDialTimeout(2*time.Second))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(c.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := c.DescribeTopic(ctx, "whatever"); !errors.Is(err, ErrKafkaUnreachable) {
		t.Errorf("DescribeTopic(dead broker) = %v, want ErrKafkaUnreachable", err)
	}
}
