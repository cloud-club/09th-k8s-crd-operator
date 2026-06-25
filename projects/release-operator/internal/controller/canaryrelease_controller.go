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
	"fmt"
	"reflect"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	deployv1alpha1 "github.com/cloud-club/09th-k8s-crd-operator/projects/release-operator/api/v1alpha1"
)

// CanaryReleaseReconciler reconciles a CanaryRelease object
type CanaryReleaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	canaryReleaseFinalizer = "deploy.canary.com/canaryrelease-finalizer"
	canaryReleaseLabel     = "canary-release"
	failureCountAnnotation = "deploy.canary.com/failure-count"
	lastFailureAnnotation  = "deploy.canary.com/last-failure-time"
)

// +kubebuilder:rbac:groups=deploy.canary.com,resources=canaryreleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=deploy.canary.com,resources=canaryreleases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=deploy.canary.com,resources=canaryreleases/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the CanaryRelease object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *CanaryReleaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var canaryRelease deployv1alpha1.CanaryRelease
	if err := r.Get(ctx, req.NamespacedName, &canaryRelease); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	applyDefaults(&canaryRelease)

	log.Info("reconciling CanaryRelease",
		"name", canaryRelease.Name,
		"namespace", canaryRelease.Namespace,
		"generation", canaryRelease.Generation,
	)

	if !canaryRelease.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalizeCanaryRelease(ctx, &canaryRelease)
	}

	if !controllerutil.ContainsFinalizer(&canaryRelease, canaryReleaseFinalizer) {
		controllerutil.AddFinalizer(&canaryRelease, canaryReleaseFinalizer)
		return ctrl.Result{}, r.Update(ctx, &canaryRelease)
	}

	if canaryRelease.Spec.StableRef.Name == "" {
		statusErr := r.updateStatusIfChanged(ctx, &canaryRelease, func() {
			setPhase(&canaryRelease, deployv1alpha1.PhasePending)
			setMessage(&canaryRelease, "waiting for stableRef.name")
			setCondition(&canaryRelease, ConditionProgressing, metav1.ConditionFalse, "WaitingForStableRef", "stableRef.name is required")
			setCondition(&canaryRelease, ConditionPromoted, metav1.ConditionFalse, "NotPromoted", "canary release has not been promoted")
			setObservedGeneration(&canaryRelease)
		})
		return ctrl.Result{}, statusErr
	}

	var stableDeployment appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: canaryRelease.Namespace,
		Name:      canaryRelease.Spec.StableRef.Name,
	}, &stableDeployment); err != nil {
		if apierrors.IsNotFound(err) {
			statusErr := r.updateStatusIfChanged(ctx, &canaryRelease, func() {
				setPhase(&canaryRelease, deployv1alpha1.PhasePending)
				msg := fmt.Sprintf("waiting for stable Deployment %q", canaryRelease.Spec.StableRef.Name)
				setMessage(&canaryRelease, msg)
				setCondition(&canaryRelease, ConditionProgressing, metav1.ConditionFalse, "WaitingForStableDeployment", msg)
				setCondition(&canaryRelease, ConditionPromoted, metav1.ConditionFalse, "NotPromoted", "canary release has not been promoted")
				setObservedGeneration(&canaryRelease)
			})
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err
	}

	canaryDeployment, err := r.ensureCanaryDeployment(ctx, &canaryRelease, &stableDeployment)
	if err != nil {
		return ctrl.Result{}, err
	}

	if len(canaryRelease.Spec.Steps) == 0 {
		statusErr := r.updateStatusIfChanged(ctx, &canaryRelease, func() {
			setPhase(&canaryRelease, deployv1alpha1.PhasePending)
			setMessage(&canaryRelease, "waiting for at least one canary step")
			setCondition(&canaryRelease, ConditionProgressing, metav1.ConditionFalse, "WaitingForCanaryStep", "at least one canary step is required")
			setCondition(&canaryRelease, ConditionPromoted, metav1.ConditionFalse, "NotPromoted", "canary release has not been promoted")
			setObservedGeneration(&canaryRelease)
		})
		return ctrl.Result{}, statusErr
	}

	if canaryRelease.Spec.FailurePolicy.Action == deployv1alpha1.FailureActionPause &&
		canaryRelease.Status.Phase == deployv1alpha1.PhaseDegraded &&
		canaryRelease.Status.ObservedGeneration == canaryRelease.Generation {
		return ctrl.Result{RequeueAfter: healthCheckInterval(&canaryRelease)}, nil
	}
	if canaryRelease.Status.Phase == deployv1alpha1.PhaseRolledBack &&
		canaryRelease.Status.ObservedGeneration == canaryRelease.Generation {
		if err := r.rollbackCanary(ctx, &canaryRelease, &stableDeployment, canaryDeployment); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	stepIndex, requeueAfter := nextStep(&canaryRelease, time.Now())
	weight := canaryRelease.Spec.Steps[stepIndex].Weight
	stableReplicas, canaryReplicas := calculateReplicas(canaryRelease.Spec.TotalReplicas, weight)
	if err := r.scaleDeployments(ctx, &stableDeployment, canaryDeployment, stableReplicas, canaryReplicas); err != nil {
		return ctrl.Result{}, err
	}

	failed, failureMessage, err := r.canaryUnhealthy(ctx, &canaryRelease, canaryDeployment)
	if err != nil {
		return ctrl.Result{}, err
	}
	if failed {
		now := time.Now()
		if lastFailureTime, ok := canaryLastFailureTime(&canaryRelease); ok {
			elapsed := now.Sub(lastFailureTime)
			if elapsed < healthCheckInterval(&canaryRelease) {
				return ctrl.Result{RequeueAfter: healthCheckInterval(&canaryRelease) - elapsed}, nil
			}
		}

		failureCount := canaryFailureCount(&canaryRelease) + 1
		setCanaryLastFailureTime(&canaryRelease, now)
		if failureCount < canaryRelease.Spec.HealthCheck.FailureThreshold {
			setCanaryFailureCount(&canaryRelease, failureCount)
			if err := r.Update(ctx, &canaryRelease); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: healthCheckInterval(&canaryRelease)}, nil
		}
		clearCanaryFailureCount(&canaryRelease)
		if err := r.Update(ctx, &canaryRelease); err != nil {
			return ctrl.Result{}, err
		}
		return r.handleCanaryFailure(ctx, &canaryRelease, &stableDeployment, canaryDeployment, failureMessage)
	}
	if canaryFailureCount(&canaryRelease) > 0 {
		clearCanaryFailureCount(&canaryRelease)
		if err := r.Update(ctx, &canaryRelease); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.updateStatusIfChanged(ctx, &canaryRelease, func() {
		msg := stepMessage(&canaryRelease, stepIndex, weight, requeueAfter)
		if isLastStep(&canaryRelease, stepIndex) {
			setPhase(&canaryRelease, deployv1alpha1.PhasePromoted)
			setCondition(&canaryRelease, ConditionProgressing, metav1.ConditionFalse, "PromotionComplete", msg)
			setCondition(&canaryRelease, ConditionPromoted, metav1.ConditionTrue, "PromotionComplete", msg)
		} else {
			setPhase(&canaryRelease, deployv1alpha1.PhaseProgressing)
			setCondition(&canaryRelease, ConditionProgressing, metav1.ConditionTrue, "StepApplied", msg)
			setCondition(&canaryRelease, ConditionPromoted, metav1.ConditionFalse, "PromotionInProgress", msg)
		}
		setCondition(&canaryRelease, ConditionDegraded, metav1.ConditionFalse, "HealthCheckPending", "canary health check has not reported a failure")
		setStepStatusIfNeeded(&canaryRelease, stepIndex, weight)
		setStableImage(&canaryRelease, firstContainerImage(&stableDeployment))
		setReplicasStatus(&canaryRelease, stableReplicas, canaryReplicas)
		setMessage(&canaryRelease, msg)
		setObservedGeneration(&canaryRelease)
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *CanaryReleaseReconciler) canaryUnhealthy(ctx context.Context, cr *deployv1alpha1.CanaryRelease, canary *appsv1.Deployment) (bool, string, error) {
	if deploymentReplicas(canary) == 0 {
		return false, "", nil
	}
	if canary.Status.UnavailableReplicas > cr.Spec.HealthCheck.MaxUnavailableCanary {
		return true, fmt.Sprintf("canary Deployment has %d unavailable replicas", canary.Status.UnavailableReplicas), nil
	}
	if cr.Spec.HealthCheck.PodRestartThreshold == 0 {
		return false, "", nil
	}

	var pods corev1.PodList
	if err := r.List(ctx, &pods,
		client.InNamespace(cr.Namespace),
		client.MatchingLabels(canary.Spec.Selector.MatchLabels),
	); err != nil {
		return false, "", err
	}

	for _, pod := range pods.Items {
		for _, status := range pod.Status.ContainerStatuses {
			if status.RestartCount >= cr.Spec.HealthCheck.PodRestartThreshold {
				return true, fmt.Sprintf("canary Pod %q container %q restarted %d times", pod.Name, status.Name, status.RestartCount), nil
			}
		}
	}

	return false, "", nil
}

func (r *CanaryReleaseReconciler) handleCanaryFailure(ctx context.Context, cr *deployv1alpha1.CanaryRelease, stable, canary *appsv1.Deployment, failureMessage string) (ctrl.Result, error) {
	switch cr.Spec.FailurePolicy.Action {
	case deployv1alpha1.FailureActionPause:
		if err := r.updateStatusIfChanged(ctx, cr, func() {
			setFailureStatus(cr, deployv1alpha1.PhaseDegraded, "CanaryPaused", failureMessage)
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: healthCheckInterval(cr)}, nil
	default:
		if err := r.rollbackCanary(ctx, cr, stable, canary); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.updateStatusIfChanged(ctx, cr, func() {
			setFailureStatus(cr, deployv1alpha1.PhaseRolledBack, "CanaryRolledBack", failureMessage)
			setReplicasStatus(cr, deploymentReplicas(stable), deploymentReplicas(canary))
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
}

func (r *CanaryReleaseReconciler) rollbackCanary(ctx context.Context, cr *deployv1alpha1.CanaryRelease, stable, canary *appsv1.Deployment) error {
	if cr.Spec.FailurePolicy.RestoreStableReplicas {
		if err := r.scaleDeployment(ctx, stable, cr.Spec.TotalReplicas); err != nil {
			return err
		}
	}
	if cr.Spec.FailurePolicy.DeleteCanaryOnRollback {
		return r.deleteCanaryDeployment(ctx, cr)
	}
	if cr.Spec.FailurePolicy.ScaleCanaryToZero {
		return r.scaleDeployment(ctx, canary, 0)
	}
	return nil
}

func setFailureStatus(cr *deployv1alpha1.CanaryRelease, phase deployv1alpha1.CanaryPhase, reason, message string) {
	setPhase(cr, phase)
	setMessage(cr, message)
	setCondition(cr, ConditionProgressing, metav1.ConditionFalse, reason, message)
	setCondition(cr, ConditionPromoted, metav1.ConditionFalse, reason, "canary release was not promoted")
	setCondition(cr, ConditionDegraded, metav1.ConditionTrue, reason, message)
	setObservedGeneration(cr)
}

func healthCheckInterval(cr *deployv1alpha1.CanaryRelease) time.Duration {
	return time.Duration(cr.Spec.HealthCheck.CheckIntervalSeconds) * time.Second
}

func canaryFailureCount(cr *deployv1alpha1.CanaryRelease) int32 {
	if cr.Annotations == nil {
		return 0
	}
	value, ok := cr.Annotations[failureCountAnnotation]
	if !ok {
		return 0
	}
	count, err := strconv.ParseInt(value, 10, 32)
	if err != nil || count < 0 {
		return 0
	}
	return int32(count)
}

func setCanaryFailureCount(cr *deployv1alpha1.CanaryRelease, count int32) {
	if cr.Annotations == nil {
		cr.Annotations = map[string]string{}
	}
	cr.Annotations[failureCountAnnotation] = strconv.FormatInt(int64(count), 10)
}

func canaryLastFailureTime(cr *deployv1alpha1.CanaryRelease) (time.Time, bool) {
	if cr.Annotations == nil {
		return time.Time{}, false
	}
	value, ok := cr.Annotations[lastFailureAnnotation]
	if !ok {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func setCanaryLastFailureTime(cr *deployv1alpha1.CanaryRelease, failureTime time.Time) {
	if cr.Annotations == nil {
		cr.Annotations = map[string]string{}
	}
	cr.Annotations[lastFailureAnnotation] = failureTime.UTC().Format(time.RFC3339)
}

func clearCanaryFailureCount(cr *deployv1alpha1.CanaryRelease) {
	if cr.Annotations == nil {
		return
	}
	delete(cr.Annotations, failureCountAnnotation)
	delete(cr.Annotations, lastFailureAnnotation)
}

func (r *CanaryReleaseReconciler) finalizeCanaryRelease(ctx context.Context, cr *deployv1alpha1.CanaryRelease) error {
	if !controllerutil.ContainsFinalizer(cr, canaryReleaseFinalizer) {
		return nil
	}

	if cr.Spec.FailurePolicy.DeleteCanaryOnDelete {
		if err := r.deleteCanaryDeployment(ctx, cr); err != nil {
			return err
		}
	}

	controllerutil.RemoveFinalizer(cr, canaryReleaseFinalizer)
	return r.Update(ctx, cr)
}

func (r *CanaryReleaseReconciler) deleteCanaryDeployment(ctx context.Context, cr *deployv1alpha1.CanaryRelease) error {
	var canaryDeployment appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: cr.Namespace,
		Name:      canaryDeploymentName(cr),
	}, &canaryDeployment); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return client.IgnoreNotFound(r.Delete(ctx, &canaryDeployment))
}

func (r *CanaryReleaseReconciler) scaleDeployments(ctx context.Context, stable, canary *appsv1.Deployment, stableReplicas, canaryReplicas int32) error {
	if err := r.scaleDeployment(ctx, stable, stableReplicas); err != nil {
		return err
	}
	return r.scaleDeployment(ctx, canary, canaryReplicas)
}

func (r *CanaryReleaseReconciler) scaleDeployment(ctx context.Context, deployment *appsv1.Deployment, replicas int32) error {
	if deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == replicas {
		return nil
	}
	deployment.Spec.Replicas = &replicas
	return r.Update(ctx, deployment)
}

func (r *CanaryReleaseReconciler) ensureCanaryDeployment(ctx context.Context, cr *deployv1alpha1.CanaryRelease, stable *appsv1.Deployment) (*appsv1.Deployment, error) {
	desired := buildCanaryDeployment(cr, stable)
	if err := controllerutil.SetControllerReference(cr, desired, r.Scheme); err != nil {
		return nil, err
	}

	var existing appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKeyFromObject(desired), &existing); err != nil {
		if apierrors.IsNotFound(err) {
			return desired, r.Create(ctx, desired)
		}
		return nil, err
	}

	ownerReferences := existing.OwnerReferences
	if err := controllerutil.SetControllerReference(cr, &existing, r.Scheme); err != nil {
		return nil, err
	}

	changed := false
	if !reflect.DeepEqual(ownerReferences, existing.OwnerReferences) {
		changed = true
	}
	if !reflect.DeepEqual(existing.Labels, desired.Labels) {
		existing.Labels = desired.Labels
		changed = true
	}
	if !reflect.DeepEqual(existing.Spec.Template, desired.Spec.Template) {
		existing.Spec.Template = desired.Spec.Template
		changed = true
	}
	if !changed {
		return &existing, nil
	}

	if err := r.Update(ctx, &existing); err != nil {
		return nil, err
	}
	return &existing, nil
}

func buildCanaryDeployment(cr *deployv1alpha1.CanaryRelease, stable *appsv1.Deployment) *appsv1.Deployment {
	labels := copyStringMap(stable.Labels)
	labels[canaryReleaseLabel] = cr.Name

	selectorLabels := copyStringMap(stable.Spec.Selector.MatchLabels)
	selectorLabels[canaryReleaseLabel] = cr.Name

	template := stable.Spec.Template.DeepCopy()
	if template.Labels == nil {
		template.Labels = map[string]string{}
	}
	for key, value := range selectorLabels {
		template.Labels[key] = value
	}
	if len(template.Spec.Containers) > 0 {
		template.Spec.Containers[0].Image = cr.Spec.Image
	}
	useCanaryConfigMaps(template)

	replicas := int32(0)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      canaryDeploymentName(cr),
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels},
			Template: *template,
		},
	}
}

func (r *CanaryReleaseReconciler) updateStatusIfChanged(ctx context.Context, cr *deployv1alpha1.CanaryRelease, update func()) error {
	before := cr.Status
	update()
	if reflect.DeepEqual(before, cr.Status) {
		return nil
	}
	return r.Status().Update(ctx, cr)
}

func currentStepIndex(cr *deployv1alpha1.CanaryRelease) int32 {
	if cr.Status.CurrentStepIndex < 0 {
		return 0
	}
	if int(cr.Status.CurrentStepIndex) >= len(cr.Spec.Steps) {
		return int32(len(cr.Spec.Steps) - 1)
	}
	return cr.Status.CurrentStepIndex
}

func nextStep(cr *deployv1alpha1.CanaryRelease, now time.Time) (int32, time.Duration) {
	stepIndex := currentStepIndex(cr)
	if !cr.Spec.AutoPromotion || isLastStep(cr, stepIndex) {
		return stepIndex, 0
	}
	if cr.Status.LastStepTime == nil {
		return stepIndex, cr.Spec.Interval.Duration
	}

	elapsed := now.Sub(cr.Status.LastStepTime.Time)
	if elapsed < cr.Spec.Interval.Duration {
		return stepIndex, cr.Spec.Interval.Duration - elapsed
	}

	stepIndex++
	if isLastStep(cr, stepIndex) {
		return stepIndex, 0
	}
	return stepIndex, cr.Spec.Interval.Duration
}

func isLastStep(cr *deployv1alpha1.CanaryRelease, stepIndex int32) bool {
	return int(stepIndex) >= len(cr.Spec.Steps)-1
}

func setStepStatusIfNeeded(cr *deployv1alpha1.CanaryRelease, stepIndex int32, weight int32) {
	if cr.Status.CurrentStepIndex == stepIndex && cr.Status.CurrentWeight == weight && cr.Status.LastStepTime != nil {
		return
	}
	setStepStatus(cr, stepIndex, weight)
}

func stepMessage(cr *deployv1alpha1.CanaryRelease, stepIndex int32, weight int32, requeueAfter time.Duration) string {
	if isLastStep(cr, stepIndex) {
		return fmt.Sprintf("promoted canary at weight %d%%", weight)
	}
	if !cr.Spec.AutoPromotion {
		return fmt.Sprintf("applied canary weight %d%% and waiting for manual promotion", weight)
	}
	return fmt.Sprintf("applied canary weight %d%%; next step in %s", weight, requeueAfter.Round(time.Second))
}

func canaryDeploymentName(cr *deployv1alpha1.CanaryRelease) string {
	return cr.Spec.StableRef.Name + "-canary"
}

func copyStringMap(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src)+1)
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func useCanaryConfigMaps(template *corev1.PodTemplateSpec) {
	for index := range template.Spec.Volumes {
		configMap := template.Spec.Volumes[index].ConfigMap
		if configMap == nil || configMap.Name == "" {
			continue
		}
		if configMap.Name == "stable-html" {
			configMap.Name = "canary-html"
		}
	}
}

func firstContainerImage(deployment *appsv1.Deployment) string {
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return ""
	}
	return deployment.Spec.Template.Spec.Containers[0].Image
}

func deploymentReplicas(deployment *appsv1.Deployment) int32 {
	if deployment.Spec.Replicas == nil {
		return 1
	}
	return *deployment.Spec.Replicas
}

// SetupWithManager sets up the controller with the Manager.
func (r *CanaryReleaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&deployv1alpha1.CanaryRelease{}).
		Owns(&appsv1.Deployment{}).
		Named("canaryrelease").
		Complete(r)
}
