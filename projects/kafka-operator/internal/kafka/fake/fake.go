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

// Package fake provides an in-memory implementation of kafka.Client for
// controller unit tests. It satisfies the same contracts as a real client
// (sentinel errors, immutable returns) but keeps no network state.
package fake

import (
	"context"
	"maps"
	"sort"
	"sync"

	"github.com/cloud-club/09th-k8s-crd-operator/projects/kafka-operator/internal/kafka"
)

// Client is a goroutine-safe, in-memory kafka.Client.
type Client struct {
	mu     sync.Mutex
	topics map[string]*kafka.TopicInfo
}

// New returns an empty fake client.
func New() *Client {
	return &Client{topics: make(map[string]*kafka.TopicInfo)}
}

// DescribeTopic implements kafka.Client.
func (c *Client) DescribeTopic(_ context.Context, name string) (*kafka.TopicInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.topics[name]
	if !ok {
		return nil, kafka.ErrTopicNotFound
	}
	return cloneTopic(t), nil
}

// CreateTopic implements kafka.Client.
func (c *Client) CreateTopic(_ context.Context, spec kafka.TopicSpec) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.topics[spec.Name]; exists {
		return kafka.ErrTopicAlreadyExists
	}
	c.topics[spec.Name] = &kafka.TopicInfo{
		Name:              spec.Name,
		Partitions:        spec.Partitions,
		ReplicationFactor: spec.ReplicationFactor,
		Config:            maps.Clone(spec.Config),
	}
	return nil
}

// DeleteTopic implements kafka.Client.
func (c *Client) DeleteTopic(_ context.Context, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.topics[name]; !ok {
		return kafka.ErrTopicNotFound
	}
	delete(c.topics, name)
	return nil
}

// UpdateConfig implements kafka.Client. Only the supplied keys are overridden;
// existing keys not present in the map are left untouched.
func (c *Client) UpdateConfig(_ context.Context, name string, config map[string]string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.topics[name]
	if !ok {
		return kafka.ErrTopicNotFound
	}
	if t.Config == nil {
		t.Config = make(map[string]string, len(config))
	}
	for k, v := range config {
		t.Config[k] = v
	}
	return nil
}

// AddPartitions implements kafka.Client.
func (c *Client) AddPartitions(_ context.Context, name string, total int32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.topics[name]
	if !ok {
		return kafka.ErrTopicNotFound
	}
	if total < t.Partitions {
		return &kafka.PartitionDecreaseError{Current: t.Partitions, Desired: total}
	}
	t.Partitions = total
	return nil
}

// Names returns the sorted set of stored topic names. Test helper.
func (c *Client) Names() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.topics))
	for k := range c.topics {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Seed inserts a topic directly, bypassing CreateTopic. Test helper.
func (c *Client) Seed(t kafka.TopicInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.topics[t.Name] = &kafka.TopicInfo{
		Name:              t.Name,
		Partitions:        t.Partitions,
		ReplicationFactor: t.ReplicationFactor,
		Config:            maps.Clone(t.Config),
	}
}

func cloneTopic(t *kafka.TopicInfo) *kafka.TopicInfo {
	return &kafka.TopicInfo{
		Name:              t.Name,
		Partitions:        t.Partitions,
		ReplicationFactor: t.ReplicationFactor,
		Config:            maps.Clone(t.Config),
	}
}

// Compile-time check that *Client satisfies kafka.Client.
var _ kafka.Client = (*Client)(nil)
