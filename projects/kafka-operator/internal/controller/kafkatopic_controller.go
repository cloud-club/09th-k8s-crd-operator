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
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kafkav1alpha1 "github.com/cloud-club/09th-k8s-crd-operator/projects/kafka-operator/api/v1alpha1"
	"github.com/cloud-club/09th-k8s-crd-operator/projects/kafka-operator/internal/kafka"
)

// finalizerName guards external cleanup: while present, deleting the CR triggers
// reconcileDelete to remove the Kafka topic before the object is purged.
const finalizerName = "kafka.study.dev/finalizer"

// Condition types and reasons surfaced on KafkaTopic.status.conditions.
const (
	conditionReady         = "Ready"
	conditionConfigDrifted = "ConfigDrifted"

	reasonTopicSynced       = "TopicSynced"
	reasonKafkaUnreachable  = "KafkaUnreachable"
	reasonPartitionDecrease = "PartitionDecreaseNotAllowed"
	reasonDriftDetected     = "DriftDetected"
	reasonConfigInSync      = "InSync"

	// requeueOnUnreachable backs off when the broker is down.
	requeueOnUnreachable = 30 * time.Second
	// createPropagationRequeue waits out Kafka metadata propagation after a
	// create when Describe still reports the topic as absent.
	createPropagationRequeue = 2 * time.Second
	// resyncInterval re-checks live Kafka so manual drift self-heals even
	// without a spec change to trigger reconciliation.
	resyncInterval = 1 * time.Minute
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

// Reconcile drives a single KafkaTopic toward its desired state:
//   - ensures a finalizer so the Kafka topic is cleaned up on CR deletion;
//   - creates the topic if absent;
//   - reconciles partition count (increase only; decrease is rejected);
//   - corrects topic config drift (spec.config vs live Kafka);
//   - reflects the outcome via Ready / ConfigDrifted conditions and observed fields.
func (r *KafkaTopicReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var kt kafkav1alpha1.KafkaTopic
	if err := r.Get(ctx, req.NamespacedName, &kt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Deletion: run external cleanup while the finalizer is held.
	if !kt.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &kt)
	}

	// Ensure the finalizer is present before touching Kafka. Adding it is its own
	// write; return so the resulting update re-triggers reconciliation and each
	// pass issues at most one write to the object (avoids resourceVersion conflicts).
	if controllerutil.AddFinalizer(&kt, finalizerName) {
		if err := r.Update(ctx, &kt); err != nil {
			return writeResult(ctrl.Result{}, err)
		}
		return ctrl.Result{}, nil
	}

	info, descErr := r.Kafka.DescribeTopic(ctx, kt.Spec.TopicName)
	switch {
	case errors.Is(descErr, kafka.ErrTopicNotFound):
		if err := r.Kafka.CreateTopic(ctx, toTopicSpec(&kt)); err != nil {
			if errors.Is(err, kafka.ErrKafkaUnreachable) {
				return r.markUnreachable(ctx, &kt, err)
			}
			if errors.Is(err, kafka.ErrTopicAlreadyExists) {
				// Describe was stale (Kafka metadata propagation lag right after
				// creation). The topic exists; requeue to read its real state.
				return ctrl.Result{RequeueAfter: createPropagationRequeue}, nil
			}
			log.Error(err, "CreateTopic failed", "topic", kt.Spec.TopicName)
			return ctrl.Result{}, err
		}
		log.Info("Created topic", "topic", kt.Spec.TopicName)
		return r.markReady(ctx, &kt, "Topic created", kt.Spec.Partitions, nil)

	case errors.Is(descErr, kafka.ErrKafkaUnreachable):
		return r.markUnreachable(ctx, &kt, descErr)

	case descErr != nil:
		return ctrl.Result{}, descErr
	}

	// Topic exists: reconcile partitions, then config.
	desired := kt.Spec.Partitions
	switch {
	case desired < info.Partitions:
		return r.markPartitionDecrease(ctx, &kt, info.Partitions, desired)

	case desired > info.Partitions:
		if err := r.Kafka.AddPartitions(ctx, kt.Spec.TopicName, desired); err != nil {
			if errors.Is(err, kafka.ErrKafkaUnreachable) {
				return r.markUnreachable(ctx, &kt, err)
			}
			var pde *kafka.PartitionDecreaseError
			if errors.As(err, &pde) {
				return r.markPartitionDecrease(ctx, &kt, pde.Current, pde.Desired)
			}
			log.Error(err, "AddPartitions failed", "topic", kt.Spec.TopicName)
			return ctrl.Result{}, err
		}
		log.Info("Increased partitions", "topic", kt.Spec.TopicName, "from", info.Partitions, "to", desired)
		info.Partitions = desired
	}

	// Config drift: only keys declared in spec.config are managed.
	drift := configDrift(kt.Spec.Config, info.Config)
	if len(drift) > 0 {
		if err := r.Kafka.UpdateConfig(ctx, kt.Spec.TopicName, drift); err != nil {
			if errors.Is(err, kafka.ErrKafkaUnreachable) {
				return r.markUnreachable(ctx, &kt, err)
			}
			log.Error(err, "UpdateConfig failed", "topic", kt.Spec.TopicName)
			return ctrl.Result{}, err
		}
		log.Info("Corrected config drift", "topic", kt.Spec.TopicName, "keys", sortedKeys(drift))
	}

	return r.markReady(ctx, &kt, "Topic in sync", info.Partitions, sortedKeys(drift))
}

// reconcileDelete removes the Kafka topic, then drops the finalizer. Deleting an
// already-absent topic is treated as success (idempotent); an unreachable broker
// keeps the finalizer and requeues so the object is not orphaned in Kafka.
func (r *KafkaTopicReconciler) reconcileDelete(ctx context.Context, kt *kafkav1alpha1.KafkaTopic) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(kt, finalizerName) {
		return ctrl.Result{}, nil
	}

	if err := r.Kafka.DeleteTopic(ctx, kt.Spec.TopicName); err != nil {
		switch {
		case errors.Is(err, kafka.ErrTopicNotFound):
			log.Info("Topic already absent on delete", "topic", kt.Spec.TopicName)
		case errors.Is(err, kafka.ErrKafkaUnreachable):
			log.Info("Kafka unreachable during delete; keeping finalizer", "topic", kt.Spec.TopicName)
			return ctrl.Result{RequeueAfter: requeueOnUnreachable}, nil
		default:
			log.Error(err, "DeleteTopic failed", "topic", kt.Spec.TopicName)
			return ctrl.Result{}, err
		}
	} else {
		log.Info("Deleted topic", "topic", kt.Spec.TopicName)
	}

	controllerutil.RemoveFinalizer(kt, finalizerName)
	return writeResult(ctrl.Result{}, r.Update(ctx, kt))
}

// writeResult turns a write outcome into a reconcile result. Optimistic-lock
// conflicts (a concurrent edit raced our write) are expected: a newer version
// is already queued, so requeue quietly instead of logging a reconcile error.
func writeResult(result ctrl.Result, err error) (ctrl.Result, error) {
	if err == nil {
		return result, nil
	}
	if apierrors.IsConflict(err) {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	return ctrl.Result{}, err
}

// markReady sets Ready=True plus the ConfigDrifted condition and observed fields.
// driftedKeys is non-empty when drift was detected (and corrected) this cycle.
func (r *KafkaTopicReconciler) markReady(
	ctx context.Context, kt *kafkav1alpha1.KafkaTopic, message string, observedPartitions int32, driftedKeys []string,
) (ctrl.Result, error) {
	before := kt.Status.DeepCopy()
	meta.SetStatusCondition(&kt.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             reasonTopicSynced,
		Message:            message,
		ObservedGeneration: kt.Generation,
	})

	drifted := metav1.Condition{
		Type:               conditionConfigDrifted,
		ObservedGeneration: kt.Generation,
	}
	if len(driftedKeys) > 0 {
		drifted.Status = metav1.ConditionTrue
		drifted.Reason = reasonDriftDetected
		drifted.Message = fmt.Sprintf("corrected drift on: %s", strings.Join(driftedKeys, ", "))
	} else {
		drifted.Status = metav1.ConditionFalse
		drifted.Reason = reasonConfigInSync
		drifted.Message = "config in sync"
	}
	meta.SetStatusCondition(&kt.Status.Conditions, drifted)

	kt.Status.ObservedGeneration = kt.Generation
	kt.Status.ObservedPartitions = observedPartitions
	return writeResult(ctrl.Result{RequeueAfter: resyncInterval}, r.writeStatus(ctx, kt, before))
}

// markPartitionDecrease reports a rejected partition decrease via Ready=False.
// No requeue: the user must raise spec.partitions, which re-triggers reconcile.
func (r *KafkaTopicReconciler) markPartitionDecrease(
	ctx context.Context, kt *kafkav1alpha1.KafkaTopic, current, desired int32,
) (ctrl.Result, error) {
	before := kt.Status.DeepCopy()
	meta.SetStatusCondition(&kt.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reasonPartitionDecrease,
		Message:            fmt.Sprintf("cannot decrease partitions from %d to %d; Kafka forbids it", current, desired),
		ObservedGeneration: kt.Generation,
	})
	kt.Status.ObservedGeneration = kt.Generation
	kt.Status.ObservedPartitions = current
	return writeResult(ctrl.Result{}, r.writeStatus(ctx, kt, before))
}

func (r *KafkaTopicReconciler) markUnreachable(
	ctx context.Context, kt *kafkav1alpha1.KafkaTopic, cause error,
) (ctrl.Result, error) {
	before := kt.Status.DeepCopy()
	meta.SetStatusCondition(&kt.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reasonKafkaUnreachable,
		Message:            cause.Error(),
		ObservedGeneration: kt.Generation,
	})
	kt.Status.ObservedGeneration = kt.Generation
	return writeResult(ctrl.Result{RequeueAfter: requeueOnUnreachable}, r.writeStatus(ctx, kt, before))
}

// writeStatus persists status only when it actually changed. Skipping no-op
// writes is essential: an unconditional Status().Update would bump the
// resourceVersion, fire a watch event, and re-trigger reconcile in a tight loop.
func (r *KafkaTopicReconciler) writeStatus(
	ctx context.Context, kt *kafkav1alpha1.KafkaTopic, before *kafkav1alpha1.KafkaTopicStatus,
) error {
	if equality.Semantic.DeepEqual(before, &kt.Status) {
		return nil
	}
	return r.Status().Update(ctx, kt)
}

// configDrift returns the subset of desired keys whose value differs from (or is
// missing in) the live Kafka config. Keys not in desired are left untouched.
func configDrift(desired, actual map[string]string) map[string]string {
	if len(desired) == 0 {
		return nil
	}
	out := make(map[string]string)
	for k, want := range desired {
		if got, ok := actual[k]; !ok || got != want {
			out[k] = want
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
