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

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Schedule",type="string",JSONPath=".spec.schedule"
// +kubebuilder:printcolumn:name="ActiveRuns",type="integer",JSONPath=".status.activeRuns"
// +kubebuilder:printcolumn:name="LastSchedule",type="date",JSONPath=".status.lastScheduleTime"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// CronJobSchedule is the Schema for the cronjobschedules API.
type CronJobSchedule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CronJobScheduleSpec   `json:"spec,omitempty"`
	Status CronJobScheduleStatus `json:"status,omitempty"`
}

// CronJobScheduleSpec defines the desired state of CronJobSchedule.
type CronJobScheduleSpec struct {
	// +kubebuilder:validation:Required
	Schedule string `json:"schedule"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Tasks []Task `json:"tasks"`

	// +kubebuilder:default=5
	// +kubebuilder:validation:Minimum=1
	HistoryLimit int32 `json:"historyLimit,omitempty"`

	// +kubebuilder:default=Forbid
	// +kubebuilder:validation:Enum=Allow;Forbid;Replace
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`
}

// ConcurrencyPolicy defines how concurrent runs are handled.
// +kubebuilder:validation:Enum=Allow;Forbid;Replace
type ConcurrencyPolicy string

const (
	// AllowConcurrent allows concurrent runs regardless of previous run status.
	AllowConcurrent ConcurrencyPolicy = "Allow"
	// ForbidConcurrent skips new run if previous run is still active.
	ForbidConcurrent ConcurrencyPolicy = "Forbid"
	// ReplaceConcurrent terminates the active run and starts a new one.
	ReplaceConcurrent ConcurrencyPolicy = "Replace"
)

// Task defines a single unit of work in the DAG.
type Task struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	Image string `json:"image"`

	Command []string `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`

	// Names of tasks that must complete successfully before this task runs.
	Dependencies []string `json:"dependencies,omitempty"`

	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	Retries int32 `json:"retries,omitempty"`
}

// CronJobScheduleStatus defines the observed state of CronJobSchedule.
type CronJobScheduleStatus struct {
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`
	LastSuccessTime  *metav1.Time `json:"lastSuccessTime,omitempty"`

	// +kubebuilder:default=0
	ActiveRuns int32 `json:"activeRuns,omitempty"`

	ExecutionHistory []ExecutionRecord  `json:"executionHistory,omitempty"`
	Conditions       []metav1.Condition `json:"conditions,omitempty"`
}

// ExecutionRecord holds the result of a single cron-triggered run.
type ExecutionRecord struct {
	// +kubebuilder:validation:Required
	RunID string `json:"runId"`

	StartTime      metav1.Time  `json:"startTime"`
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// +kubebuilder:validation:Enum=Running;Succeeded;Failed
	Phase string `json:"phase"`

	TaskStatuses []TaskStatus `json:"taskStatuses,omitempty"`
}

// TaskStatus holds the execution state of a single task within a run.
type TaskStatus struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed;Skipped
	Phase string `json:"phase"`

	JobName    string `json:"jobName,omitempty"`
	RetryCount int32  `json:"retryCount,omitempty"`
}

// +kubebuilder:object:root=true

// CronJobScheduleList contains a list of CronJobSchedule.
type CronJobScheduleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CronJobSchedule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CronJobSchedule{}, &CronJobScheduleList{})
}
