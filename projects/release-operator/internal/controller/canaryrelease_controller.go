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

const canaryReleaseLabel = "canary-release"

// +kubebuilder:rbac:groups=deploy.canary.com,resources=canaryreleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=deploy.canary.com,resources=canaryreleases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=deploy.canary.com,resources=canaryreleases/finalizers,verbs=update

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

	if canaryRelease.Spec.StableRef.Name == "" {
		statusErr := r.updateStatusIfChanged(ctx, &canaryRelease, func() {
			setPhase(&canaryRelease, deployv1alpha1.PhasePending)
			setMessage(&canaryRelease, "waiting for stableRef.name")
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
				setMessage(&canaryRelease, fmt.Sprintf("waiting for stable Deployment %q", canaryRelease.Spec.StableRef.Name))
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

	if err := r.updateStatusIfChanged(ctx, &canaryRelease, func() {
		setPhase(&canaryRelease, deployv1alpha1.PhaseProgressing)
		setStableImage(&canaryRelease, firstContainerImage(&stableDeployment))
		setReplicasStatus(&canaryRelease, deploymentReplicas(&stableDeployment), deploymentReplicas(canaryDeployment))
		setMessage(&canaryRelease, fmt.Sprintf("ensured canary Deployment %q", canaryDeployment.Name))
		setObservedGeneration(&canaryRelease)
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
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
	if !reflect.DeepEqual(existing.Spec.Replicas, desired.Spec.Replicas) {
		existing.Spec.Replicas = desired.Spec.Replicas
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
		Named("canaryrelease").
		Complete(r)
}
