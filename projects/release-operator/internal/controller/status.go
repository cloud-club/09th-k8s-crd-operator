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

package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	deployv1alpha1 "github.com/cloud-club/09th-k8s-crd-operator/projects/release-operator/api/v1alpha1"
)

// setPhase는 CanaryRelease의 status.phase를 갱신한다.
func setPhase(cr *deployv1alpha1.CanaryRelease, phase deployv1alpha1.CanaryPhase) {
	cr.Status.Phase = phase
}

// setStepStatus는 현재 step 인덱스와 canary 비율을 갱신한다.
func setStepStatus(cr *deployv1alpha1.CanaryRelease, stepIndex int32, weight int32) {
	cr.Status.CurrentStepIndex = stepIndex
	cr.Status.CurrentWeight = weight
	now := metav1.Now()
	cr.Status.LastStepTime = &now
}

// setReplicasStatus는 stable/canary replica 수를 갱신한다.
func setReplicasStatus(cr *deployv1alpha1.CanaryRelease, stable, canary int32) {
	cr.Status.StableReplicas = stable
	cr.Status.CanaryReplicas = canary
}

// setStableImage는 stable Deployment가 실행 중인 이미지를 기록한다.
func setStableImage(cr *deployv1alpha1.CanaryRelease, image string) {
	cr.Status.StableImage = image
}

// setMessage는 사람이 읽을 수 있는 상태 메시지를 갱신한다.
func setMessage(cr *deployv1alpha1.CanaryRelease, msg string) {
	cr.Status.Message = msg
}

// setObservedGeneration은 현재 spec.generation을 status에 기록한다.
// Reconcile 완료 시 호출해 "이 spec 세대까지 반영했다"는 것을 표시한다.
func setObservedGeneration(cr *deployv1alpha1.CanaryRelease) {
	cr.Status.ObservedGeneration = cr.Generation
}

// applyDefaults는 사용자가 healthCheck / failurePolicy 블록을 생략했을 때
// zero 값인 필드를 기본값으로 보정한다.
//
// kubebuilder default 마커는 블록 자체가 없으면 중첩 필드까지 적용되지 않으므로
// Reconcile 진입부에서 이 함수를 호출해 보정한다.
func applyDefaults(cr *deployv1alpha1.CanaryRelease) {
	hc := &cr.Spec.HealthCheck
	if hc.CheckIntervalSeconds == 0 {
		hc.CheckIntervalSeconds = 10
	}
	if hc.FailureThreshold == 0 {
		hc.FailureThreshold = 3
	}
	if hc.PodRestartThreshold == 0 {
		hc.PodRestartThreshold = 1
	}

	fp := &cr.Spec.FailurePolicy
	if fp.Action == "" {
		fp.Action = deployv1alpha1.FailureActionRollback
	}
	if !fp.RestoreStableReplicas {
		fp.RestoreStableReplicas = true
	}
	if !fp.ScaleCanaryToZero {
		fp.ScaleCanaryToZero = true
	}
	if !fp.DeleteCanaryOnDelete {
		fp.DeleteCanaryOnDelete = true
	}
}
