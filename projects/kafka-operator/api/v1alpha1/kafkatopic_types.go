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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KafkaTopicSpec defines the desired state of KafkaTopic.
type KafkaTopicSpec struct {
	// TopicName is the actual topic name in Kafka.
	// Must match Kafka's naming rules: 1-249 chars of [a-zA-Z0-9._-]. Immutable.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=249
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9._-]+$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="topicName is immutable"
	// +required
	TopicName string `json:"topicName"`

	// Partitions is the desired partition count.
	// Kafka does not allow partition decrease; the controller surfaces such
	// attempts via the Ready=False / PartitionDecreaseNotAllowed condition
	// rather than rejecting them at admission.
	// +kubebuilder:validation:Minimum=1
	// +required
	Partitions int32 `json:"partitions"`

	// ReplicationFactor is the number of broker replicas per partition. Immutable.
	// Changing the replication factor requires kafka-reassign-partitions and is out of scope.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="replicationFactor is immutable"
	// +required
	ReplicationFactor int16 `json:"replicationFactor"`

	// Config holds topic-level Kafka configuration overrides (e.g. retention.ms).
	// Only keys present here are managed by the operator; absent keys keep Kafka defaults.
	// +optional
	Config map[string]string `json:"config,omitempty"`
}

// KafkaTopicStatus defines the observed state of KafkaTopic.
type KafkaTopicStatus struct {
	// Conditions represent the latest observations of the KafkaTopic state.
	// Standard types:
	//   - Ready: the topic is in sync with Kafka
	//   - ConfigDrifted: spec.config diverges from the live Kafka config (week 2)
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the .metadata.generation last processed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ObservedPartitions is the partition count observed in Kafka at the last reconcile.
	// +optional
	ObservedPartitions int32 `json:"observedPartitions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Topic",type=string,JSONPath=`.spec.topicName`
// +kubebuilder:printcolumn:name="Partitions",type=integer,JSONPath=`.spec.partitions`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// KafkaTopic is the Schema for the kafkatopics API.
type KafkaTopic struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of KafkaTopic
	// +required
	Spec KafkaTopicSpec `json:"spec"`

	// status defines the observed state of KafkaTopic
	// +optional
	Status KafkaTopicStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// KafkaTopicList contains a list of KafkaTopic.
type KafkaTopicList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []KafkaTopic `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KafkaTopic{}, &KafkaTopicList{})
}
