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

// CanaryReleaseSpec defines the desired state of CanaryRelease.
type CanaryReleaseSpec struct {
	// 기존 stable Deployment를 가리킨다.
	// +kubebuilder:validation:Required
	StableRef ResourceRef `json:"stableRef"`

	// stable Pod와 canary Pod를 함께 바라볼 Service를 가리킨다.
	// +kubebuilder:validation:Required
	ServiceRef ServiceRef `json:"serviceRef"`

	// 배포할 새 버전 canary 컨테이너 이미지
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// 앱이 listen 하는 컨테이너 포트
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=8080
	Port int32 `json:"port,omitempty"`

	// stable + canary의 총 replica 수
	// 예) totalReplicas=10, weight=30 → canary 3 / stable 7
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=50
	// +kubebuilder:default=10
	TotalReplicas int32 `json:"totalReplicas,omitempty"`

	// canary 비중을 단계적으로 올리는 설정
	// 예) [{weight: 10}, {weight: 30}, {weight: 60}, {weight: 100}]
	// +kubebuilder:validation:MinItems=1
	Steps []CanaryStep `json:"steps"`

	// 다음 단계로 넘어가기 전 대기 시간. 예) "30s", "2m"
	// +kubebuilder:validation:Type=string
	// +kubebuilder:default="30s"
	Interval metav1.Duration `json:"interval,omitempty"`

	// canary가 정상일 때 다음 단계로 자동 진행할지 여부
	// +kubebuilder:default=true
	AutoPromotion bool `json:"autoPromotion,omitempty"`

	// canary 정상/비정상 판단 기준
	// +optional
	HealthCheck HealthCheckSpec `json:"healthCheck,omitempty"`

	// 실패, rollback, 삭제 시 정리 정책
	// +optional
	FailurePolicy FailurePolicySpec `json:"failurePolicy,omitempty"`
}

// ResourceRef는 기존 stable Deployment 같은 Kubernetes 리소스를 가리킨다.
type ResourceRef struct {
	// +kubebuilder:default="apps/v1"
	APIVersion string `json:"apiVersion,omitempty"`

	// +kubebuilder:default="Deployment"
	Kind string `json:"kind,omitempty"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// ServiceRef는 stable/canary Pod를 함께 바라볼 Service를 가리킨다.
type ServiceRef struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// CanaryStep은 canary 비율 한 단계를 의미한다.
type CanaryStep struct {
	// 해당 단계의 canary 비율
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	Weight int32 `json:"weight"`
}

// HealthCheckSpec은 canary 상태 판단 기준이다.
type HealthCheckSpec struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=10
	CheckIntervalSeconds int32 `json:"checkIntervalSeconds,omitempty"`

	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=3
	FailureThreshold int32 `json:"failureThreshold,omitempty"`

	// 허용 가능한 canary unavailable Pod 수
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=0
	MaxUnavailableCanary int32 `json:"maxUnavailableCanary,omitempty"`

	// canary Pod 재시작 횟수가 이 값 이상이면 실패로 본다.
	// 0이면 재시작 횟수 기반 판단을 사용하지 않는다.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	PodRestartThreshold int32 `json:"podRestartThreshold,omitempty"`
}

// FailurePolicySpec은 실패, rollback, 삭제 시 동작을 정의한다.
type FailurePolicySpec struct {
	// 실패 시 동작
	// Rollback: stable 상태로 되돌림 / Pause: 멈추고 사람이 판단
	// +kubebuilder:validation:Enum=Rollback;Pause
	// +kubebuilder:default=Rollback
	Action FailureActionType `json:"action,omitempty"`

	// rollback 시 stable replica를 원래 비율로 복구할지 여부
	// +kubebuilder:default=true
	RestoreStableReplicas bool `json:"restoreStableReplicas,omitempty"`

	// rollback 시 canary Deployment를 0개로 줄일지 여부
	// +kubebuilder:default=true
	ScaleCanaryToZero bool `json:"scaleCanaryToZero,omitempty"`

	// rollback 시 canary Deployment 자체를 삭제할지 여부
	// +kubebuilder:default=false
	DeleteCanaryOnRollback bool `json:"deleteCanaryOnRollback,omitempty"`

	// CanaryRelease 삭제 시 canary Deployment를 삭제할지 여부
	// +kubebuilder:default=true
	DeleteCanaryOnDelete bool `json:"deleteCanaryOnDelete,omitempty"`
}

// +kubebuilder:validation:Enum=Rollback;Pause
type FailureActionType string

const (
	FailureActionRollback FailureActionType = "Rollback"
	FailureActionPause    FailureActionType = "Pause"
)

// +kubebuilder:validation:Enum=Pending;Progressing;Promoting;Promoted;RollingBack;RolledBack;Degraded
type CanaryPhase string

const (
	PhasePending     CanaryPhase = "Pending"
	PhaseProgressing CanaryPhase = "Progressing"
	PhasePromoting   CanaryPhase = "Promoting"
	PhasePromoted    CanaryPhase = "Promoted"
	PhaseRollingBack CanaryPhase = "RollingBack"
	PhaseRolledBack  CanaryPhase = "RolledBack"
	PhaseDegraded    CanaryPhase = "Degraded"
)

// CanaryReleaseStatus defines the observed state of CanaryRelease.
type CanaryReleaseStatus struct {
	// 현재 상태 머신 단계
	// +optional
	Phase CanaryPhase `json:"phase,omitempty"`

	// 현재 진행 중인 steps의 인덱스
	// +optional
	CurrentStepIndex int32 `json:"currentStepIndex,omitempty"`

	// 현재 canary 비율
	// +optional
	CurrentWeight int32 `json:"currentWeight,omitempty"`

	// 현재 stable Deployment replica 수
	// +optional
	StableReplicas int32 `json:"stableReplicas,omitempty"`

	// 현재 canary Deployment replica 수
	// +optional
	CanaryReplicas int32 `json:"canaryReplicas,omitempty"`

	// 현재 stable Deployment가 실행 중인 이미지
	// +optional
	StableImage string `json:"stableImage,omitempty"`

	// 마지막 단계 전이 시각
	// +optional
	LastStepTime *metav1.Time `json:"lastStepTime,omitempty"`

	// 마지막으로 Reconcile이 반영한 metadata.generation
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// 사람이 읽을 수 있는 현재 상태 메시지
	// +optional
	Message string `json:"message,omitempty"`

	// 다축 상태 정보 (Progressing, Promoted, Degraded 등)
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=canaryreleases,scope=Namespaced,shortName=canary
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Step",type="integer",JSONPath=".status.currentStepIndex"
// +kubebuilder:printcolumn:name="Weight",type="integer",JSONPath=".status.currentWeight"
// +kubebuilder:printcolumn:name="Stable",type="integer",JSONPath=".status.stableReplicas"
// +kubebuilder:printcolumn:name="Canary",type="integer",JSONPath=".status.canaryReplicas"
// +kubebuilder:printcolumn:name="AutoPromotion",type="boolean",JSONPath=".spec.autoPromotion"
// +kubebuilder:printcolumn:name="Image",type="string",JSONPath=".spec.image"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type CanaryRelease struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CanaryReleaseSpec   `json:"spec,omitempty"`
	Status CanaryReleaseStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type CanaryReleaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CanaryRelease `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CanaryRelease{}, &CanaryReleaseList{})
}
