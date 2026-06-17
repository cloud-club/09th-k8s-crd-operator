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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	appv1 "github.com/withyou/my-operator/api/v1"
)

const myFinalizer = "app.example.com/finalizer"

// MyAppReconciler reconciles a MyApp object
type MyAppReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=app.example.com,resources=myapps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=app.example.com,resources=myapps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=app.example.com,resources=myapps/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

func (r *MyAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// ── 1단계: MyApp 오브젝트 조회 ──────────────────────────
	myApp := &appv1.MyApp{}
	if err := r.Get(ctx, req.NamespacedName, myApp); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// ── 2단계: 삭제 중인지 확인 (Finalizer) ─────────────────
	// DeletionTimestamp 가 찍혀있으면 삭제 대기 중
	if !myApp.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(myApp, myFinalizer) {
			log.Info("삭제 전 cleanup 실행", "name", myApp.Name)

			// 외부 리소스 정리 로직 (예: AWS S3, 외부 DB 등)
			// 이 스터디에서는 로그로 시뮬레이션
			log.Info("외부 리소스 정리 완료 (시뮬레이션)", "name", myApp.Name)

			// cleanup 완료 → Finalizer 제거
			controllerutil.RemoveFinalizer(myApp, myFinalizer)
			if err := r.Update(ctx, myApp); err != nil {
				return ctrl.Result{}, err
			}
			log.Info("Finalizer 제거 완료, 오브젝트 삭제 진행", "name", myApp.Name)
		}
		return ctrl.Result{}, nil
	}

	// ── 3단계: Finalizer 등록 ────────────────────────────────
	if !controllerutil.ContainsFinalizer(myApp, myFinalizer) {
		controllerutil.AddFinalizer(myApp, myFinalizer)
		if err := r.Update(ctx, myApp); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("Finalizer 등록 완료", "name", myApp.Name)
	}

	log.Info("Reconcile 시작", "name", myApp.Name, "replicas", myApp.Spec.Replicas, "image", myApp.Spec.Image)

	// ── 4단계: Deployment 조회 ──────────────────────────────
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      myApp.Name,
		Namespace: myApp.Namespace,
	}, deployment)

	// ── 5단계: Desired vs Current 비교 후 조정 ──────────────
	if errors.IsNotFound(err) {
		log.Info("Deployment 없음, 새로 생성", "name", myApp.Name)

		newDeploy := r.buildDeployment(myApp)

		if err := ctrl.SetControllerReference(myApp, newDeploy, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.Create(ctx, newDeploy); err != nil {
			log.Error(err, "Deployment 생성 실패")
			return ctrl.Result{}, err
		}

		log.Info("Deployment 생성 완료", "name", myApp.Name)

	} else if err != nil {
		return ctrl.Result{}, err

	} else {
		needUpdate := false

		if *deployment.Spec.Replicas != myApp.Spec.Replicas {
			log.Info("replicas 변경 감지",
				"현재", *deployment.Spec.Replicas,
				"목표", myApp.Spec.Replicas,
			)
			deployment.Spec.Replicas = &myApp.Spec.Replicas
			needUpdate = true
		}

		currentImage := deployment.Spec.Template.Spec.Containers[0].Image
		if currentImage != myApp.Spec.Image {
			log.Info("image 변경 감지",
				"현재", currentImage,
				"목표", myApp.Spec.Image,
			)
			deployment.Spec.Template.Spec.Containers[0].Image = myApp.Spec.Image
			needUpdate = true
		}

		if needUpdate {
			if err := r.Update(ctx, deployment); err != nil {
				log.Error(err, "Deployment 업데이트 실패")
				return ctrl.Result{}, err
			}
			log.Info("Deployment 업데이트 완료", "name", myApp.Name)
		} else {
			log.Info("변경 없음, 아무것도 안 함", "name", myApp.Name)
		}
	}

	// ── 6단계: Status 업데이트 ──────────────────────────────
	updatedDeploy := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      myApp.Name,
		Namespace: myApp.Namespace,
	}, updatedDeploy); err == nil {
		myApp.Status.ReadyReplicas = updatedDeploy.Status.ReadyReplicas
		myApp.Status.ObservedGeneration = myApp.Generation

		// Phase 설정
		if updatedDeploy.Status.ReadyReplicas == myApp.Spec.Replicas {
			myApp.Status.Phase = "Running"
		} else {
			myApp.Status.Phase = "Pending"
		}

		// Ready Condition 설정
		conditionStatus := metav1.ConditionFalse
		reason := "DeploymentNotReady"
		message := fmt.Sprintf("%d/%d replicas ready", updatedDeploy.Status.ReadyReplicas, myApp.Spec.Replicas)

		if updatedDeploy.Status.ReadyReplicas == myApp.Spec.Replicas {
			conditionStatus = metav1.ConditionTrue
			reason = "DeploymentReady"
			message = fmt.Sprintf("All %d replicas are ready", myApp.Spec.Replicas)
		}

		apimeta.SetStatusCondition(&myApp.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             conditionStatus,
			LastTransitionTime: metav1.Now(),
			Reason:             reason,
			Message:            message,
		})

		if err := r.Status().Update(ctx, myApp); err != nil {
			log.Error(err, "Status 업데이트 실패")
			return ctrl.Result{}, err
		}
		log.Info("Status 업데이트 완료",
			"readyReplicas", myApp.Status.ReadyReplicas,
			"phase", myApp.Status.Phase,
			"observedGeneration", myApp.Status.ObservedGeneration,
		)
	}

	return ctrl.Result{}, nil
}

func (r *MyAppReconciler) buildDeployment(myApp *appv1.MyApp) *appsv1.Deployment {
	labels := map[string]string{"app": myApp.Name}
	replicas := myApp.Spec.Replicas

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      myApp.Name,
			Namespace: myApp.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: myApp.Spec.Image,
						},
					},
				},
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *MyAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appv1.MyApp{}).
		Owns(&appsv1.Deployment{}).
		Named("myapp").
		Complete(r)
}
