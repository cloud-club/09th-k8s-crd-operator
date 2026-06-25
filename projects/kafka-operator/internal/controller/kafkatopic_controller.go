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

// api server의 etcd에서 CR을 지우기 전에 kafka의 토픽부터 지우기 위함
// CR의 metadata에서 관리
const finalizerName = "kafka.study.dev/finalizer"

// KafkaTopic 리소스의 status.conditions 항목에 표시되는 조건 유형과 상세 사유
const (
	conditionReady         = "Ready"
	conditionConfigDrifted = "ConfigDrifted"

	reasonTopicSynced       = "TopicSynced"
	reasonKafkaUnreachable  = "KafkaUnreachable"
	reasonPartitionDecrease = "PartitionDecreaseNotAllowed"
	reasonDriftDetected     = "DriftDetected"
	reasonConfigInSync      = "InSync"

	// 브로커가 다운된 경우 30초 후 requeue
	requeueOnUnreachable = 30 * time.Second
	// 토픽 생성 직후 메타데이터 전파 지연을 기다리는 2초
	createPropagationRequeue = 2 * time.Second
	// Drift 없어도 1분마다 재확인, 외부 리소스라 watch 주기적으로 해야함.
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

// - ensures a finalizer so the Kafka topic is cleaned up on CR deletion;
// - creates the topic if absent;
// - reconciles partition count (increase only; decrease is rejected);
// - corrects topic config drift (spec.config vs live Kafka);
// - reflects the outcome via Ready / ConfigDrifted conditions and observed fields.
func (r *KafkaTopicReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var kt kafkav1alpha1.KafkaTopic
	if err := r.Get(ctx, req.NamespacedName, &kt); err != nil {
		// 요청된 KafkaTopic CR을 API서버에서 읽음
		// 이미 삭제돼 없으면 IgnoreNotFound가 에러를 nil로 바꿔서 requeue를 막는다
		// 다른 에러면 그대로 반환, 자동 재시도
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// DeletionTimestamp 찍혀 있으면 삭제. 삭제 처리 함수 reconcileDelete 호출
	if !kt.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &kt)
	}

	// finalizer가 없으면 붙이고 true 반환 (이미 있으면 false, 블록 스킵)
	if controllerutil.AddFinalizer(&kt, finalizerName) {
		// finalizer를 붙였으면 Update로 저장한다
		// 이 Update가 또 watch 이벤트를 일으켜 reconcile이 다시 돌아간다
		// (finalizer는 metadata에서 관리되는데, r.Status().Update를 쓰면 metadata 접근이 불가능함)
		if err := r.Update(ctx, &kt); err != nil {
			return writeResult(ctrl.Result{}, err)
		}
		return ctrl.Result{}, nil
	}

	// Kafka의 실제 상태(observed) 조회. 이후 desired와 비교.
	info, descErr := r.Kafka.DescribeTopic(ctx, kt.Spec.TopicName)
	switch {
	// 에러 1: 토픽이 없음, 생성 시도
	case errors.Is(descErr, kafka.ErrTopicNotFound):
		if err := r.Kafka.CreateTopic(ctx, toTopicSpec(&kt)); err != nil {
			// 브로커 접근 실패면 Ready=False(KafkaUnreachable) 찍고 30초 뒤 재시도.
			if errors.Is(err, kafka.ErrKafkaUnreachable) {
				return r.markUnreachable(ctx, &kt, err)
			}
			// Describe가 메타데이터 전파 지연으로 stale했던 것
			// 2초 뒤 requeue 해서 진짜 상태를 다시 읽음.
			if errors.Is(err, kafka.ErrTopicAlreadyExists) {
				return ctrl.Result{RequeueAfter: createPropagationRequeue}, nil
			}
			log.Error(err, "CreateTopic failed", "topic", kt.Spec.TopicName)
			return ctrl.Result{}, err
		}
		log.Info("Created topic", "topic", kt.Spec.TopicName)
		return r.markReady(ctx, &kt, "Topic created", kt.Spec.Partitions, nil)

	// 에러 2: 카프카 브로커 접근 실패(Unreachable)
	case errors.Is(descErr, kafka.ErrKafkaUnreachable):
		return r.markUnreachable(ctx, &kt, descErr)

	// 나머지 에러 처리
	case descErr != nil:
		return ctrl.Result{}, descErr
	}

	// 파티션 reconcile 시작
	desired := kt.Spec.Partitions
	switch {
	// 파티션을 감소시킬 수는 없음. 처리 불가
	case desired < info.Partitions:
		return r.markPartitionDecrease(ctx, &kt, info.Partitions, desired)

	// 파티션 추가
	case desired > info.Partitions:
		if err := r.Kafka.AddPartitions(ctx, kt.Spec.TopicName, desired); err != nil {
			if errors.Is(err, kafka.ErrKafkaUnreachable) {
				return r.markUnreachable(ctx, &kt, err)
			}
			var pde *kafka.PartitionDecreaseError
			if errors.As(err, &pde) {
				// race condition 때문에 desired < info.Partitions로 바뀌어버린 경우
				return r.markPartitionDecrease(ctx, &kt, pde.Current, pde.Desired)
			}
			log.Error(err, "AddPartitions failed", "topic", kt.Spec.TopicName)
			return ctrl.Result{}, err
		}
		log.Info("Increased partitions", "topic", kt.Spec.TopicName, "from", info.Partitions, "to", desired)
		info.Partitions = desired
	}

	// Config reconcile 시작
	// spec.config에 선언된 config만 관리
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

	// 파티션/config 수렴 성공, Ready=True로 바꾸기
	// sortedKeys(drift)를 넘겨 "어떤 드리프트를 고쳤는지"를 ConfigDrifted condition에 기록.
	return r.markReady(ctx, &kt, "Topic in sync", info.Partitions, sortedKeys(drift))
}

// 토픽 삭제 처리
func (r *KafkaTopicReconciler) reconcileDelete(ctx context.Context, kt *kafkav1alpha1.KafkaTopic) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 다른 finalizer가 처리 중이거나 이미 정리됨
	if !controllerutil.ContainsFinalizer(kt, finalizerName) {
		return ctrl.Result{}, nil
	}
	// 토픽 삭제
	if err := r.Kafka.DeleteTopic(ctx, kt.Spec.TopicName); err != nil {
		switch {
		// 이미 없으면 성공으로 간주(멱등)
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
		// 토픽 삭제 성공
		log.Info("Deleted topic", "topic", kt.Spec.TopicName)
	}

	// finalizer 제거 후 Update
	// finalizer가 사라지고 reconcile이 다시 돌면 CR이 삭제된다
	controllerutil.RemoveFinalizer(kt, finalizerName)
	return writeResult(ctrl.Result{}, r.Update(ctx, kt))
}

// 쓰기 담당. reconcile에서 호출
func writeResult(result ctrl.Result, err error) (ctrl.Result, error) {
	// 에러 없으면 그대로 진행
	if err == nil {
		return result, nil
	}
	// Optimistic Lock 충돌(그새 누가 오브젝트를 수정해 resourceVersion이 어긋남)은 정상 상황
	// 에러 로그 없이 1초 뒤 재시도
	if apierrors.IsConflict(err) {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	return ctrl.Result{}, err
}

// Ready=True condition 세팅. SetStatusCondition은 같은 Type이 있으면 갱신하되,
// Status가 안 바뀌면 LastTransitionTime은 유지(불필요한 변경 방지)
func (r *KafkaTopicReconciler) markReady(
	ctx context.Context, kt *kafkav1alpha1.KafkaTopic, message string, observedPartitions int32, driftedKeys []string,
) (ctrl.Result, error) {
	// 변경 전 status 스냅샷 (나중에 정말 바뀌었는지 비교용)
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
	// Drifted condition: 드리프트를 고친 직후라도 이 사이클엔 True로 뜨고, 1분 뒤 resync 때 False로 내려간다
	// 의미가 "드리프트 감지"보다 "이번에 감지/교정함"에 가까움. 사소한 특징
	meta.SetStatusCondition(&kt.Status.Conditions, drifted)

	// 무한루프 방지를 위해 ObservedGeneration, ObservedPartitions 동기화
	kt.Status.ObservedGeneration = kt.Generation
	kt.Status.ObservedPartitions = observedPartitions
	// resyncInterval, 외부 드리프트 주기적 감지
	return writeResult(ctrl.Result{RequeueAfter: resyncInterval}, r.writeStatus(ctx, kt, before))
}

// PartitionDecreaseNotAllowed condition 세팅
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

// KafkaUnreachable condition 세팅
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

// Status 쓰기 담당. Status 내용 같으면 건너 뜀
func (r *KafkaTopicReconciler) writeStatus(
	ctx context.Context, kt *kafkav1alpha1.KafkaTopic, before *kafkav1alpha1.KafkaTopicStatus,
) error {
	if equality.Semantic.DeepEqual(before, &kt.Status) {
		return nil
	}
	return r.Status().Update(ctx, kt)
}

// configDrift 추출하는 헬퍼 함수
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
