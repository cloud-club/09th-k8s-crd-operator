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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	appv1 "github.com/withyou/my-operator/api/v1"
)

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
			// MyApp이 삭제된 경우 → 정상 종료
			log.Info("MyApp 없음, 삭제된 것으로 판단", "name", req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log.Info("Reconcile 시작", "name", myApp.Name, "replicas", myApp.Spec.Replicas, "image", myApp.Spec.Image)

	// ── 2단계: Deployment 조회 ──────────────────────────────
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      myApp.Name,
		Namespace: myApp.Namespace,
	}, deployment)

	// ── 3단계: Desired vs Current 비교 후 조정 ──────────────
	if errors.IsNotFound(err) {
		// Deployment가 없음 → 새로 생성
		log.Info("Deployment 없음, 새로 생성", "name", myApp.Name)

		newDeploy := r.buildDeployment(myApp)

		// OwnerReference 설정: MyApp이 삭제되면 Deployment도 자동 삭제
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
		// Deployment가 이미 있음 → 변경 여부 확인
		needUpdate := false

		// replicas 변경 여부 확인
		if *deployment.Spec.Replicas != myApp.Spec.Replicas {
			log.Info("replicas 변경 감지",
				"현재", *deployment.Spec.Replicas,
				"목표", myApp.Spec.Replicas,
			)
			deployment.Spec.Replicas = &myApp.Spec.Replicas
			needUpdate = true
		}

		// image 변경 여부 확인
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

	// ── 4단계: Status 업데이트 ──────────────────────────────
	// 최신 Deployment 상태 재조회
	updatedDeploy := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      myApp.Name,
		Namespace: myApp.Namespace,
	}, updatedDeploy); err == nil {
		myApp.Status.ReadyReplicas = updatedDeploy.Status.ReadyReplicas
		if updatedDeploy.Status.ReadyReplicas == myApp.Spec.Replicas {
			myApp.Status.Phase = "Running"
		} else {
			myApp.Status.Phase = "Pending"
		}

		if err := r.Status().Update(ctx, myApp); err != nil {
			log.Error(err, "Status 업데이트 실패")
			return ctrl.Result{}, err
		}
		log.Info("Status 업데이트 완료",
			"readyReplicas", myApp.Status.ReadyReplicas,
			"phase", myApp.Status.Phase,
		)
	}

	return ctrl.Result{}, nil
}

// buildDeployment: MyApp spec을 기반으로 Deployment 오브젝트 생성
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
		Owns(&appsv1.Deployment{}). // Deployment 변경 시 MyApp Reconcile 트리거
		Named("myapp").
		Complete(r)
}
