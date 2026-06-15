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

// Package kafka provides the domain abstraction over Kafka admin operations
// used by the KafkaTopic controller. The interface is intentionally narrow:
// idempotency, retry, and condition management are the caller's responsibility,
// while this package surfaces precise state via typed errors.
//
// The contract lives in docs/kafka-client-interface.md.
package kafka

import (
	"context"
	"errors"
	"fmt"
)

// Sentinel errors. Wrappers may add context but must Unwrap to one of these
// so callers can branch on errors.Is.
var (
	// ErrTopicNotFound is returned when the target topic does not exist.
	ErrTopicNotFound = errors.New("kafka: topic not found")

	// ErrTopicAlreadyExists is returned by CreateTopic when the topic exists.
	ErrTopicAlreadyExists = errors.New("kafka: topic already exists")

	// ErrKafkaUnreachable indicates the broker could not be reached
	// (network, auth, timeout). Callers typically Requeue.
	ErrKafkaUnreachable = errors.New("kafka: broker unreachable")

	// ErrPartitionDecrease indicates an attempt to lower the partition count,
	// which Kafka does not allow. Concrete attempts return PartitionDecreaseError
	// which Unwraps to this sentinel.
	ErrPartitionDecrease = errors.New("kafka: partition decrease not allowed")
)

// PartitionDecreaseError carries the current and desired partition counts.
// Recover via errors.As; check the sentinel via errors.Is(err, ErrPartitionDecrease).
type PartitionDecreaseError struct {
	Current int32
	Desired int32
}

func (e *PartitionDecreaseError) Error() string {
	return fmt.Sprintf("kafka: cannot decrease partitions from %d to %d", e.Current, e.Desired)
}

func (e *PartitionDecreaseError) Unwrap() error { return ErrPartitionDecrease }

// TopicInfo is the observed state of a Kafka topic returned by DescribeTopic.
// Config contains the full set of effective configuration keys reported by
// Kafka (overrides + defaults); callers compare only the keys they manage.
type TopicInfo struct {
	Name              string
	Partitions        int32
	ReplicationFactor int16
	Config            map[string]string
}

// TopicSpec is the input to CreateTopic.
type TopicSpec struct {
	Name              string
	Partitions        int32
	ReplicationFactor int16
	Config            map[string]string
}

// Client is the domain abstraction over Kafka admin operations.
//
// Method contracts:
//
//   - DescribeTopic returns ErrTopicNotFound when the topic does not exist.
//   - CreateTopic returns ErrTopicAlreadyExists when the topic exists.
//   - DeleteTopic returns ErrTopicNotFound when the topic does not exist;
//     callers convert to nil for finalizer idempotency.
//   - UpdateConfig overrides only the supplied keys (incremental AlterConfigs);
//     keys absent from the map are left untouched.
//   - AddPartitions raises the total partition count; equal is a noop;
//     lower returns *PartitionDecreaseError.
//
// All methods may return ErrKafkaUnreachable when the broker is unreachable.
// Implementations must be safe for concurrent use.
type Client interface {
	DescribeTopic(ctx context.Context, name string) (*TopicInfo, error)
	CreateTopic(ctx context.Context, spec TopicSpec) error
	DeleteTopic(ctx context.Context, name string) error
	UpdateConfig(ctx context.Context, name string, config map[string]string) error
	AddPartitions(ctx context.Context, name string, total int32) error
}
