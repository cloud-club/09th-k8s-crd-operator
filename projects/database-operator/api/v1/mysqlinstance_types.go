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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MySQLInstanceSpec defines the desired state of MySQLInstance.
type MySQLInstanceSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum="5.7";"8.0";"8.4"
	Version string `json:"version"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	Replicas int32 `json:"replicas"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^\d+(Mi|Gi)$`
	// +kubebuilder:default="1Gi"
	StorageSize string `json:"storageSize"`

	// +kubebuilder:validation:Required
	RootPasswordSecret string   `json:"rootPasswordSecret"`
	InitSQL            []string `json:"initSQL,omitempty"`
	BackupSchedule     string   `json:"backupSchedule,omitempty"`
}

// MySQLInstanceStatus defines the observed state of MySQLInstance.
type MySQLInstanceStatus struct {
	// +kubebuilder:validation:Enum=Pending;Creating;Running;Failed
	Phase string `json:"phase,omitempty"`

	ReadyReplicas int32  `json:"readyReplicas,omitempty"`
	ServiceName   string `json:"serviceName,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// MySQLInstance is the Schema for the mysqlinstances API.
type MySQLInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MySQLInstanceSpec   `json:"spec,omitempty"`
	Status MySQLInstanceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MySQLInstanceList contains a list of MySQLInstance.
type MySQLInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MySQLInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MySQLInstance{}, &MySQLInstanceList{})
}
