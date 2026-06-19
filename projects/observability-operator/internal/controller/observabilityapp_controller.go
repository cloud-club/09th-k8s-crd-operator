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
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	observabilityv1 "github.com/cloud-club/09th-k8s-crd-operator/projects/observability-operator/api/v1"
)

// ObservabilityAppReconciler reconciles a ObservabilityApp object
type ObservabilityAppReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=observability.example.com,resources=observabilityapps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=observability.example.com,resources=observabilityapps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=observability.example.com,resources=observabilityapps/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop.
// 현재 단계에서는 ObservabilityApp CR을 기준으로 같은 Namespace 안의 Deployment와 Service를 조회하고,
// 그 결과를 Status Conditions에 반영한다.
func (r *ObservabilityAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1. ObservabilityApp CR 조회
	observabilityApp := &observabilityv1.ObservabilityApp{}
	if err := r.Get(ctx, req.NamespacedName, observabilityApp); err != nil {
		if apierrors.IsNotFound(err) {
			// CR이 삭제된 경우에는 더 이상 처리할 대상이 없으므로 정상 종료한다.
			return ctrl.Result{}, nil
		}

		// API 서버 조회 중 실제 에러가 발생한 경우에는 에러를 반환한다.
		return ctrl.Result{}, err
	}

	log.Info(
		"Reconciling ObservabilityApp",
		"name", observabilityApp.Name,
		"namespace", observabilityApp.Namespace,
	)

	// 2. Status 기본값 초기화
	observabilityApp.Status.ObservedGeneration = observabilityApp.Generation
	observabilityApp.Status.Phase = "Progressing"
	observabilityApp.Status.Score = 0
	observabilityApp.Status.MetricsReady = false
	observabilityApp.Status.LogsReady = false
	observabilityApp.Status.DashboardReady = false
	observabilityApp.Status.TracesReady = false
	observabilityApp.Status.Recommendations = []string{}

	// 3. 현재는 Deployment만 targetRef.kind로 지원한다.
	if observabilityApp.Spec.TargetRef.Kind != "Deployment" {
		observabilityApp.Status.Phase = "Failed"
		observabilityApp.Status.Recommendations = append(
			observabilityApp.Status.Recommendations,
			"Only Deployment targetRef.kind is supported currently.",
		)

		meta.SetStatusCondition(&observabilityApp.Status.Conditions, metav1.Condition{
			Type:               "TargetKindSupported",
			Status:             metav1.ConditionFalse,
			Reason:             "UnsupportedTargetKind",
			Message:            "Only Deployment targetRef.kind is supported currently.",
			ObservedGeneration: observabilityApp.Generation,
			LastTransitionTime: metav1.Now(),
		})

		if err := r.Status().Update(ctx, observabilityApp); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	meta.SetStatusCondition(&observabilityApp.Status.Conditions, metav1.Condition{
		Type:               "TargetKindSupported",
		Status:             metav1.ConditionTrue,
		Reason:             "SupportedTargetKind",
		Message:            "Deployment targetRef.kind is supported.",
		ObservedGeneration: observabilityApp.Generation,
		LastTransitionTime: metav1.Now(),
	})

	// 4. 같은 Namespace 안에서 targetRef.name에 해당하는 Deployment 조회
	deployment := &appsv1.Deployment{}
	deploymentFound := true

	if err := r.Get(ctx, client.ObjectKey{
		Namespace: observabilityApp.Namespace,
		Name:      observabilityApp.Spec.TargetRef.Name,
	}, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			deploymentFound = false
		} else {
			return ctrl.Result{}, err
		}
	}

	if deploymentFound {
		observabilityApp.Status.Score += 30

		meta.SetStatusCondition(&observabilityApp.Status.Conditions, metav1.Condition{
			Type:               "TargetDeploymentFound",
			Status:             metav1.ConditionTrue,
			Reason:             "DeploymentFound",
			Message:            "Target Deployment was found.",
			ObservedGeneration: observabilityApp.Generation,
			LastTransitionTime: metav1.Now(),
		})
	} else {
		observabilityApp.Status.Recommendations = append(
			observabilityApp.Status.Recommendations,
			"Target Deployment was not found. Check spec.targetRef.name.",
		)

		meta.SetStatusCondition(&observabilityApp.Status.Conditions, metav1.Condition{
			Type:               "TargetDeploymentFound",
			Status:             metav1.ConditionFalse,
			Reason:             "DeploymentNotFound",
			Message:            "Target Deployment was not found.",
			ObservedGeneration: observabilityApp.Generation,
			LastTransitionTime: metav1.Now(),
		})
	}

	// 5. 같은 Namespace 안에서 targetRef.name에 해당하는 Service 조회
	// 현재는 Deployment 이름과 Service 이름이 같다는 전제로 조회한다.
	service := &corev1.Service{}
	serviceFound := true

	if err := r.Get(ctx, client.ObjectKey{
		Namespace: observabilityApp.Namespace,
		Name:      observabilityApp.Spec.TargetRef.Name,
	}, service); err != nil {
		if apierrors.IsNotFound(err) {
			serviceFound = false
		} else {
			return ctrl.Result{}, err
		}
	}

	if serviceFound {
		observabilityApp.Status.Score += 30

		meta.SetStatusCondition(&observabilityApp.Status.Conditions, metav1.Condition{
			Type:               "TargetServiceFound",
			Status:             metav1.ConditionTrue,
			Reason:             "ServiceFound",
			Message:            "Target Service was found.",
			ObservedGeneration: observabilityApp.Generation,
			LastTransitionTime: metav1.Now(),
		})

		// Metrics가 enabled이고 Service가 존재하면 1주차 기준으로 MetricsReady를 true 처리한다.
		if observabilityApp.Spec.Metrics.Enabled {
			observabilityApp.Status.MetricsReady = true
			observabilityApp.Status.Score += 20

			meta.SetStatusCondition(&observabilityApp.Status.Conditions, metav1.Condition{
				Type:               "MetricsConfigured",
				Status:             metav1.ConditionTrue,
				Reason:             "MetricsEnabled",
				Message:            "Metrics is enabled and target Service was found.",
				ObservedGeneration: observabilityApp.Generation,
				LastTransitionTime: metav1.Now(),
			})
		} else {
			observabilityApp.Status.MetricsReady = false

			meta.SetStatusCondition(&observabilityApp.Status.Conditions, metav1.Condition{
				Type:               "MetricsConfigured",
				Status:             metav1.ConditionFalse,
				Reason:             "MetricsDisabled",
				Message:            "Metrics is disabled in spec.metrics.enabled.",
				ObservedGeneration: observabilityApp.Generation,
				LastTransitionTime: metav1.Now(),
			})
		}
	} else {
		observabilityApp.Status.MetricsReady = false
		observabilityApp.Status.Recommendations = append(
			observabilityApp.Status.Recommendations,
			"Target Service was not found. Create a Service for metrics scraping.",
		)

		meta.SetStatusCondition(&observabilityApp.Status.Conditions, metav1.Condition{
			Type:               "TargetServiceFound",
			Status:             metav1.ConditionFalse,
			Reason:             "ServiceNotFound",
			Message:            "Target Service was not found.",
			ObservedGeneration: observabilityApp.Generation,
			LastTransitionTime: metav1.Now(),
		})

		meta.SetStatusCondition(&observabilityApp.Status.Conditions, metav1.Condition{
			Type:               "MetricsConfigured",
			Status:             metav1.ConditionFalse,
			Reason:             "ServiceNotFound",
			Message:            "Metrics cannot be configured because target Service was not found.",
			ObservedGeneration: observabilityApp.Generation,
			LastTransitionTime: metav1.Now(),
		})
	}

	// 6. Logs 상태 반영
	// 현재 1주차에서는 실제 label/annotation 적용 전이므로,
	// Deployment가 있고 logs.enabled가 true이면 준비된 것으로 간단히 판단한다.
	if observabilityApp.Spec.Logs.Enabled && deploymentFound {
		observabilityApp.Status.LogsReady = true
		observabilityApp.Status.Score += 20

		meta.SetStatusCondition(&observabilityApp.Status.Conditions, metav1.Condition{
			Type:               "LogsConfigured",
			Status:             metav1.ConditionTrue,
			Reason:             "LogsEnabled",
			Message:            "Logs is enabled and target Deployment was found.",
			ObservedGeneration: observabilityApp.Generation,
			LastTransitionTime: metav1.Now(),
		})
	} else {
		observabilityApp.Status.LogsReady = false

		meta.SetStatusCondition(&observabilityApp.Status.Conditions, metav1.Condition{
			Type:               "LogsConfigured",
			Status:             metav1.ConditionFalse,
			Reason:             "LogsNotReady",
			Message:            "Logs is disabled or target Deployment was not found.",
			ObservedGeneration: observabilityApp.Generation,
			LastTransitionTime: metav1.Now(),
		})
	}

	// 7. Dashboard 상태 반영
	// Dashboard ConfigMap 생성은 2주차 작업으로 남겨둔다.
	observabilityApp.Status.DashboardReady = false
	if observabilityApp.Spec.Dashboard.Enabled {
		observabilityApp.Status.Recommendations = append(
			observabilityApp.Status.Recommendations,
			"Dashboard creation is not implemented yet. It will be added in week 2.",
		)

		meta.SetStatusCondition(&observabilityApp.Status.Conditions, metav1.Condition{
			Type:               "DashboardConfigured",
			Status:             metav1.ConditionFalse,
			Reason:             "NotImplementedYet",
			Message:            "Dashboard creation is not implemented yet.",
			ObservedGeneration: observabilityApp.Generation,
			LastTransitionTime: metav1.Now(),
		})
	}

	// 8. Traces 상태 반영
	// Traces는 확장용 필드이므로 현재는 항상 false 처리한다.
	observabilityApp.Status.TracesReady = false
	if observabilityApp.Spec.Traces.Enabled {
		observabilityApp.Status.Recommendations = append(
			observabilityApp.Status.Recommendations,
			"Traces are not implemented yet. This field is reserved for future OpenTelemetry support.",
		)

		meta.SetStatusCondition(&observabilityApp.Status.Conditions, metav1.Condition{
			Type:               "TracesConfigured",
			Status:             metav1.ConditionFalse,
			Reason:             "NotImplementedYet",
			Message:            "Traces are not implemented yet.",
			ObservedGeneration: observabilityApp.Generation,
			LastTransitionTime: metav1.Now(),
		})
	}

	// 9. 전체 Phase 결정
	if deploymentFound && serviceFound {
		observabilityApp.Status.Phase = "Ready"
	} else {
		observabilityApp.Status.Phase = "Failed"
	}

	// 10. Status 업데이트
	if err := r.Status().Update(ctx, observabilityApp); err != nil {
		return ctrl.Result{}, err
	}

	log.Info(
		"Updated ObservabilityApp status",
		"name", observabilityApp.Name,
		"namespace", observabilityApp.Namespace,
		"phase", observabilityApp.Status.Phase,
		"score", observabilityApp.Status.Score,
	)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ObservabilityAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&observabilityv1.ObservabilityApp{}).
		Named("observabilityapp").
		Complete(r)
}
