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
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dbv1 "github.com/09th-k8s-crd-operator/projects/database-operator/api/v1"
)

const (
	mysqlFinalizer = "database.study.dev/finalizer"
	mysqlPort      = 3306
)

// MySQLInstanceReconciler reconciles a MySQLInstance object
type MySQLInstanceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=database.study.dev,resources=mysqlinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=database.study.dev,resources=mysqlinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=database.study.dev,resources=mysqlinstances/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

func (r *MySQLInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	mysql := &dbv1.MySQLInstance{}
	if err := r.Get(ctx, req.NamespacedName, mysql); err != nil {
		if errors.IsNotFound(err) {
			log.Info("MySQLInstance resource not found, skipping")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// ── Finalizer: handle deletion ──
	if mysql.ObjectMeta.DeletionTimestamp != nil {
		if controllerutil.ContainsFinalizer(mysql, mysqlFinalizer) {
			log.Info("Running finalizer cleanup for MySQLInstance")
			if err := r.cleanupResources(ctx, mysql); err != nil {
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(mysql, mysqlFinalizer)
			if err := r.Update(ctx, mysql); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(mysql, mysqlFinalizer) {
		controllerutil.AddFinalizer(mysql, mysqlFinalizer)
		if err := r.Update(ctx, mysql); err != nil {
			return ctrl.Result{}, err
		}
	}

	// ── Set initial phase ──
	if mysql.Status.Phase == "" {
		mysql.Status.Phase = "Pending"
		if err := r.Status().Update(ctx, mysql); err != nil {
			return ctrl.Result{}, err
		}
	}

	// ── Validate that the referenced Secret exists ──
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      mysql.Spec.RootPasswordSecret,
		Namespace: mysql.Namespace,
	}, secret); err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "Referenced rootPasswordSecret not found", "secret", mysql.Spec.RootPasswordSecret)
			mysql.Status.Phase = "Failed"
			_ = r.Status().Update(ctx, mysql)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	// ── Update phase to Creating ──
	if mysql.Status.Phase == "Pending" {
		mysql.Status.Phase = "Creating"
		if err := r.Status().Update(ctx, mysql); err != nil {
			return ctrl.Result{}, err
		}
	}

	// ── Reconcile PVC ──
	if err := r.reconcilePVC(ctx, mysql); err != nil {
		log.Error(err, "Failed to reconcile PVC")
		return ctrl.Result{}, err
	}

	// ── Reconcile Init SQL ConfigMap ──
	if err := r.reconcileInitSQLConfigMap(ctx, mysql); err != nil {
		log.Error(err, "Failed to reconcile InitSQL ConfigMap")
		return ctrl.Result{}, err
	}

	// ── Reconcile Deployment ──
	if err := r.reconcileDeployment(ctx, mysql); err != nil {
		log.Error(err, "Failed to reconcile Deployment")
		return ctrl.Result{}, err
	}

	// ── Reconcile Service ──
	if err := r.reconcileService(ctx, mysql); err != nil {
		log.Error(err, "Failed to reconcile Service")
		return ctrl.Result{}, err
	}

	// ── Update Status ──
	if err := r.updateStatus(ctx, mysql); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

// ──────────────────────────────────────────────
// PVC
// ──────────────────────────────────────────────

func (r *MySQLInstanceReconciler) reconcilePVC(ctx context.Context, mysql *dbv1.MySQLInstance) error {
	pvcName := fmt.Sprintf("%s-data", mysql.Name)

	pvc := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: mysql.Namespace}, pvc)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	if errors.IsNotFound(err) {
		pvc = &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: mysql.Namespace,
				Labels:    labelsForMySQL(mysql),
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse(mysql.Spec.StorageSize),
					},
				},
			},
		}

		if err := controllerutil.SetControllerReference(mysql, pvc, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, pvc)
	}

	return nil
}

// ──────────────────────────────────────────────
// Deployment
// ──────────────────────────────────────────────

func (r *MySQLInstanceReconciler) reconcileDeployment(ctx context.Context, mysql *dbv1.MySQLInstance) error {
	deployName := fmt.Sprintf("%s-mysql", mysql.Name)
	pvcName := fmt.Sprintf("%s-data", mysql.Name)

	deploy := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{Name: deployName, Namespace: mysql.Namespace}, deploy)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	labels := labelsForMySQL(mysql)
	replicas := mysql.Spec.Replicas

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "mysql-data",
			MountPath: "/var/lib/mysql",
		},
	}

	volumes := []corev1.Volume{
		{
			Name: "mysql-data",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcName,
				},
			},
		},
	}

	if len(mysql.Spec.InitSQL) > 0 {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "init-sql",
			MountPath: "/docker-entrypoint-initdb.d",
		})

		volumes = append(volumes, corev1.Volume{
			Name: "init-sql",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-init-sql", mysql.Name),
					},
				},
			},
		})
	}

	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployName,
			Namespace: mysql.Namespace,
			Labels:    labels,
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
							Name:  "mysql",
							Image: fmt.Sprintf("mysql:%s", mysql.Spec.Version),
							Ports: []corev1.ContainerPort{
								{
									Name:          "mysql",
									ContainerPort: mysqlPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "MYSQL_ROOT_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: mysql.Spec.RootPasswordSecret,
											},
											Key: "password",
										},
									},
								},
							},
							VolumeMounts: volumeMounts,
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromInt(mysqlPort),
									},
								},
								InitialDelaySeconds: 15,
								PeriodSeconds:       10,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromInt(mysqlPort),
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       15,
							},
						},
					},
					Volumes: volumes,
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(mysql, desired, r.Scheme); err != nil {
		return err
	}

	if errors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}

	// Update existing deployment if spec changed
	updated := false

	// replica sync
	if deploy.Spec.Replicas == nil || *deploy.Spec.Replicas != replicas {
		deploy.Spec.Replicas = &replicas
		updated = true
	}

	// image sync
	desiredImage := fmt.Sprintf("mysql:%s", mysql.Spec.Version)

	if len(deploy.Spec.Template.Spec.Containers) > 0 &&
		deploy.Spec.Template.Spec.Containers[0].Image != desiredImage {
		deploy.Spec.Template.Spec.Containers[0].Image = desiredImage
		mysql.Status.Phase = "Upgrading"
		_ = r.Status().Update(ctx, mysql)
		updated = true
	}

	// init sql mount sync
	if len(deploy.Spec.Template.Spec.Containers) > 0 {
		currentMounts := deploy.Spec.Template.Spec.Containers[0].VolumeMounts
		desiredMounts := desired.Spec.Template.Spec.Containers[0].VolumeMounts
		if len(currentMounts) != len(desiredMounts) {
			deploy.Spec.Template.Spec.Volumes = desired.Spec.Template.Spec.Volumes
			deploy.Spec.Template.Spec.Containers[0].VolumeMounts = desiredMounts
			updated = true
		}
	}

	if updated {
		return r.Update(ctx, deploy)
	}

	return nil
}

// ──────────────────────────────────────────────
// Service
// ──────────────────────────────────────────────

func (r *MySQLInstanceReconciler) reconcileInitSQLConfigMap(ctx context.Context, mysql *dbv1.MySQLInstance) error {
	if len(mysql.Spec.InitSQL) == 0 {
		return nil
	}

	configMapName := fmt.Sprintf("%s-init-sql", mysql.Name)

	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      configMapName,
		Namespace: mysql.Namespace,
	}, cm)

	sql := strings.Join(mysql.Spec.InitSQL, ";\n") + ";"

	if errors.IsNotFound(err) {
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: mysql.Namespace,
			},
			Data: map[string]string{
				"init.sql": sql,
			},
		}

		if err := controllerutil.SetControllerReference(mysql, cm, r.Scheme); err != nil {
			return err
		}

		return r.Create(ctx, cm)
	}

	return nil
}

func (r *MySQLInstanceReconciler) reconcileService(ctx context.Context, mysql *dbv1.MySQLInstance) error {
	svcName := fmt.Sprintf("%s-mysql", mysql.Name)

	svc := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: svcName, Namespace: mysql.Namespace}, svc)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	if errors.IsNotFound(err) {
		svc = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: mysql.Namespace,
				Labels:    labelsForMySQL(mysql),
			},
			Spec: corev1.ServiceSpec{
				Selector: labelsForMySQL(mysql),
				Ports: []corev1.ServicePort{
					{
						Name:       "mysql",
						Port:       mysqlPort,
						TargetPort: intstr.FromInt(mysqlPort),
						Protocol:   corev1.ProtocolTCP,
					},
				},
				Type: corev1.ServiceTypeClusterIP,
			},
		}

		if err := controllerutil.SetControllerReference(mysql, svc, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, svc)
	}

	return nil
}

// ──────────────────────────────────────────────
// Status Update
// ──────────────────────────────────────────────

func (r *MySQLInstanceReconciler) updateStatus(ctx context.Context, mysql *dbv1.MySQLInstance) error {
	deployName := fmt.Sprintf("%s-mysql", mysql.Name)
	svcName := fmt.Sprintf("%s-mysql", mysql.Name)

	deploy := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: deployName, Namespace: mysql.Namespace}, deploy); err != nil {
		return err
	}

	mysql.Status.ReadyReplicas = deploy.Status.ReadyReplicas
	mysql.Status.ServiceName = svcName

	if deploy.Status.ReadyReplicas == *deploy.Spec.Replicas {
		mysql.Status.Phase = "Running"
	} else if deploy.Status.ReadyReplicas > 0 {
		mysql.Status.Phase = "Creating"
	} else {
		mysql.Status.Phase = "Pending"
	}

	return r.Status().Update(ctx, mysql)
}

// ──────────────────────────────────────────────
// Finalizer Cleanup
// ──────────────────────────────────────────────

func (r *MySQLInstanceReconciler) cleanupResources(ctx context.Context, mysql *dbv1.MySQLInstance) error {
	log := logf.FromContext(ctx)
	log.Info("Cleaning up resources for MySQLInstance", "name", mysql.Name)
	// OwnerReference handles cascading deletion automatically.
	// This hook is available for any additional cleanup (external resources, etc.)
	return nil
}

// ──────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────

func labelsForMySQL(mysql *dbv1.MySQLInstance) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "mysql",
		"app.kubernetes.io/instance":   mysql.Name,
		"app.kubernetes.io/managed-by": "database-operator",
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *MySQLInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dbv1.MySQLInstance{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Named("mysqlinstance").
		Complete(r)
}
