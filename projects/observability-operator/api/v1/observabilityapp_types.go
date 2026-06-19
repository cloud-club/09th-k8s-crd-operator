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

// TargetRef는 Observability 설정을 적용할 대상 리소스를 의미한다.
type TargetRef struct {
	// Kind는 대상 리소스의 종류이다. 예: Deployment
	Kind string `json:"kind"`

	// Name은 대상 리소스의 이름이다.
	Name string `json:"name"`
}

// MetricsSpec은 Metrics 관측 설정을 정의한다.
type MetricsSpec struct {
	// Enabled는 Metrics 관측 활성화 여부이다.
	Enabled bool `json:"enabled,omitempty"`

	// Path는 Metrics endpoint 경로이다. 예: /metrics
	Path string `json:"path,omitempty"`

	// Port는 Metrics endpoint port 이름이다. 예: http
	Port string `json:"port,omitempty"`
}

// LogsSpec은 Logs 관측 설정을 정의한다.
type LogsSpec struct {
	// Enabled는 Logs 관측 활성화 여부이다.
	Enabled bool `json:"enabled,omitempty"`
}

// DashboardSpec은 Dashboard 생성 설정을 정의한다.
type DashboardSpec struct {
	// Enabled는 Dashboard 생성 여부이다.
	Enabled bool `json:"enabled,omitempty"`
}

// TracesSpec은 Trace 관측 설정을 정의한다.
// 현재는 확장용 필드로 사용한다.
type TracesSpec struct {
	// Enabled는 Trace 관측 활성화 여부이다.
	Enabled bool `json:"enabled,omitempty"`
}

// ObservabilityAppSpec defines the desired state of ObservabilityApp.
type ObservabilityAppSpec struct {
	// TargetRef는 관측 설정을 적용할 대상 리소스를 지정한다.
	TargetRef TargetRef `json:"targetRef"`

	// Metrics는 Metrics 관측 설정이다.
	Metrics MetricsSpec `json:"metrics,omitempty"`

	// Logs는 Logs 관측 설정이다.
	Logs LogsSpec `json:"logs,omitempty"`

	// Dashboard는 Grafana Dashboard 생성 설정이다.
	Dashboard DashboardSpec `json:"dashboard,omitempty"`

	// Traces는 추후 OpenTelemetry 기반 Trace 확장을 위한 설정이다.
	Traces TracesSpec `json:"traces,omitempty"`

	// Mode는 Operator 동작 모드이다. 예: basic, full
	Mode string `json:"mode,omitempty"`
}

// ObservabilityAppStatus defines the observed state of ObservabilityApp.
type ObservabilityAppStatus struct {
	// ObservedGeneration은 현재 status가 반영한 metadata.generation 값이다.(몇번째의 변경인지 표시)
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase는 전체 처리 상태를 나타낸다. 예: Pending, Ready, Failed
	Phase string `json:"phase,omitempty"`

	// Score는 Observability 적용 점수이다.
	Score int `json:"score,omitempty"`

	// MetricsReady는 Metrics 관측 설정 준비 여부이다.
	MetricsReady bool `json:"metricsReady,omitempty"`

	// LogsReady는 Logs 관측 설정 준비 여부이다.
	LogsReady bool `json:"logsReady,omitempty"`

	// DashboardReady는 Dashboard 생성 준비 여부이다.
	DashboardReady bool `json:"dashboardReady,omitempty"`

	// TracesReady는 Trace 관측 설정 준비 여부이다.
	TracesReady bool `json:"tracesReady,omitempty"`

	// Recommendations는 설정이 부족할 때 사용자에게 보여줄 개선 권고 사항이다.
	Recommendations []string `json:"recommendations,omitempty"`

	// Conditions는 항목별 상세 상태를 나타낸다.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ObservabilityApp is the Schema for the observabilityapps API.
type ObservabilityApp struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ObservabilityAppSpec   `json:"spec,omitempty"`
	Status ObservabilityAppStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ObservabilityAppList contains a list of ObservabilityApp.
type ObservabilityAppList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ObservabilityApp `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ObservabilityApp{}, &ObservabilityAppList{})
}
