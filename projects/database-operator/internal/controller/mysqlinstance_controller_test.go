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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dbv1 "github.com/09th-k8s-crd-operator/projects/database-operator/api/v1"
)

var _ = Describe("MySQLInstance Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "team-1",
		}
		mysqlinstance := &dbv1.MySQLInstance{}

		BeforeEach(func() {
			By("creating the Secret referenced by MySQLInstance")
			secret := &corev1.Secret{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "mysql-root-secret", Namespace: "default"}, secret)
			if err != nil && errors.IsNotFound(err) {
				secret = &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mysql-root-secret",
						Namespace: "team-1",
					},
					StringData: map[string]string{
						"password": "testpassword",
					},
				}
				Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			}

			By("creating the custom resource for the Kind MySQLInstance")
			err = k8sClient.Get(ctx, typeNamespacedName, mysqlinstance)
			if err != nil && errors.IsNotFound(err) {
				resource := &dbv1.MySQLInstance{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "team-1",
					},
					Spec: dbv1.MySQLInstanceSpec{
						Version:            "8.0",
						Replicas:           1,
						StorageSize:        "1Gi",
						RootPasswordSecret: "mysql-root-secret",
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &dbv1.MySQLInstance{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the MySQLInstance")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Cleanup the Secret")
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "mysql-root-secret", Namespace: "default"}, secret)
			if err == nil {
				Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			}
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &MySQLInstanceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that a Deployment was created")
			deploy := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-mysql",
				Namespace: "team-1",
			}, deploy)
			Expect(err).NotTo(HaveOccurred())
			Expect(*deploy.Spec.Replicas).To(Equal(int32(1)))

			By("Checking that a Service was created")
			svc := &corev1.Service{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-mysql",
				Namespace: "team-1",
			}, svc)
			Expect(err).NotTo(HaveOccurred())

			By("Checking that a PVC was created")
			pvc := &corev1.PersistentVolumeClaim{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-data",
				Namespace: "team-1",
			}, pvc)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
