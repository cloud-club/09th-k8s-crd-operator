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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kafkav1alpha1 "github.com/cloud-club/09th-k8s-crd-operator/projects/kafka-operator/api/v1alpha1"
	"github.com/cloud-club/09th-k8s-crd-operator/projects/kafka-operator/internal/kafka/fake"
)

var _ = Describe("KafkaTopic Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		kafkatopic := &kafkav1alpha1.KafkaTopic{}

		var fakeKafka *fake.Client
		var controllerReconciler *KafkaTopicReconciler

		// reconcileUntilSettled runs Reconcile a few times so multi-pass flows
		// (finalizer add → create → status) converge.
		reconcileUntilSettled := func() {
			for i := 0; i < 3; i++ {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).NotTo(HaveOccurred())
			}
		}

		BeforeEach(func() {
			fakeKafka = fake.New()
			controllerReconciler = &KafkaTopicReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Kafka:  fakeKafka,
			}

			By("creating the custom resource for the Kind KafkaTopic")
			err := k8sClient.Get(ctx, typeNamespacedName, kafkatopic)
			if err != nil && errors.IsNotFound(err) {
				resource := &kafkav1alpha1.KafkaTopic{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: kafkav1alpha1.KafkaTopicSpec{
						TopicName:         "test-topic",
						Partitions:        3,
						ReplicationFactor: 1,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &kafkav1alpha1.KafkaTopic{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); errors.IsNotFound(err) {
				return
			}

			By("Cleanup the specific resource instance KafkaTopic")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Reconciling deletion so the finalizer is removed")
			reconcileUntilSettled()
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, typeNamespacedName, resource))
			}).Should(BeTrue())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			reconcileUntilSettled()

			By("Creating the topic in Kafka")
			Expect(fakeKafka.Names()).To(ContainElement("test-topic"))

			By("Setting Ready=True on status")
			updated := &kafkav1alpha1.KafkaTopic{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			Expect(meta.IsStatusConditionTrue(updated.Status.Conditions, conditionReady)).To(BeTrue())
		})
	})
})
