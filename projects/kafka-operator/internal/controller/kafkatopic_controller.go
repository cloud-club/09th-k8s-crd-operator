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
	"errors"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kafkav1alpha1 "github.com/cloud-club/09th-k8s-crd-operator/projects/kafka-operator/api/v1alpha1"
	"github.com/cloud-club/09th-k8s-crd-operator/projects/kafka-operator/internal/kafka"
)

// Condition types and reasons surfaced on KafkaTopic.status.conditions.
const (
	conditionReady = "Ready"

	reasonTopicSynced      = "TopicSynced"
	reasonKafkaUnreachable = "KafkaUnreachable"

	requeueOnUnreachable = 30 * time.Second
)

// KafkaTopicReconciler reconciles a KafkaTopic object.
type KafkaTopicReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Kafka  kafka.Client
}

// +kubebuilder:rbac:groups=kafka.study.dev,resources=kafkatopics,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kafka.study.dev,resources=kafkatopics/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kafka.study.dev,resources=kafkatopics/finalizers,verbs=update

// Reconcile drives a single KafkaTopic toward its desired state.
//
// Week 1 scope: create the topic in Kafka if absent; surface result via the
// Ready condition and ObservedGeneration / ObservedPartitions. Drift,
// partition resizing, and finalizer-based deletion land in week 2.
func (r *KafkaTopicReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var kt kafkav1alpha1.KafkaTopic
	if err := r.Get(ctx, req.NamespacedName, &kt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	info, descErr := r.Kafka.DescribeTopic(ctx, kt.Spec.TopicName)
	switch {
	case errors.Is(descErr, kafka.ErrTopicNotFound):
		if err := r.Kafka.CreateTopic(ctx, toTopicSpec(&kt)); err != nil {
			if errors.Is(err, kafka.ErrKafkaUnreachable) {
				return r.markUnreachable(ctx, &kt, err)
			}
			log.Error(err, "CreateTopic failed", "topic", kt.Spec.TopicName)
			return ctrl.Result{}, err
		}
		log.Info("Created topic", "topic", kt.Spec.TopicName)
		return r.markReady(ctx, &kt, "Topic created", kt.Spec.Partitions)

	case errors.Is(descErr, kafka.ErrKafkaUnreachable):
		return r.markUnreachable(ctx, &kt, descErr)

	case descErr != nil:
		return ctrl.Result{}, descErr

	default:
		// Topic exists. Drift handling lands in week 2.
		return r.markReady(ctx, &kt, "Topic exists", info.Partitions)
	}
}

func (r *KafkaTopicReconciler) markReady(
	ctx context.Context, kt *kafkav1alpha1.KafkaTopic, message string, observedPartitions int32,
) (ctrl.Result, error) {
	meta.SetStatusCondition(&kt.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             reasonTopicSynced,
		Message:            message,
		ObservedGeneration: kt.Generation,
	})
	kt.Status.ObservedGeneration = kt.Generation
	kt.Status.ObservedPartitions = observedPartitions
	return ctrl.Result{}, r.Status().Update(ctx, kt)
}

func (r *KafkaTopicReconciler) markUnreachable(
	ctx context.Context, kt *kafkav1alpha1.KafkaTopic, cause error,
) (ctrl.Result, error) {
	meta.SetStatusCondition(&kt.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reasonKafkaUnreachable,
		Message:            cause.Error(),
		ObservedGeneration: kt.Generation,
	})
	kt.Status.ObservedGeneration = kt.Generation
	if err := r.Status().Update(ctx, kt); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueOnUnreachable}, nil
}

func toTopicSpec(kt *kafkav1alpha1.KafkaTopic) kafka.TopicSpec {
	return kafka.TopicSpec{
		Name:              kt.Spec.TopicName,
		Partitions:        kt.Spec.Partitions,
		ReplicationFactor: kt.Spec.ReplicationFactor,
		Config:            kt.Spec.Config,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *KafkaTopicReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kafkav1alpha1.KafkaTopic{}).
		Named("kafkatopic").
		Complete(r)
}
