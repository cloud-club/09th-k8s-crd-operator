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
	"time"

	"github.com/robfig/cron/v3"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	cronv1 "github.com/cloud-club/09th-k8s-crd-operator/projects/cronschedule-operator/api/v1"
)

// CronJobScheduleReconciler reconciles a CronJobSchedule object
type CronJobScheduleReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=cron.example.com,resources=cronjobschedules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cron.example.com,resources=cronjobschedules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cron.example.com,resources=cronjobschedules/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

func (r *CronJobScheduleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1. CR 조회
	cjs := &cronv1.CronJobSchedule{}
	if err := r.Get(ctx, req.NamespacedName, cjs); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. 진행 중인 run들 상태 갱신
	if err := r.syncActiveRuns(ctx, cjs); err != nil {
		return ctrl.Result{}, err
	}

	// 3. activeRuns 카운트 갱신
	activeCount := int32(0)
	for _, record := range cjs.Status.ExecutionHistory {
		if record.Phase == "Running" {
			activeCount++
		}
	}
	cjs.Status.ActiveRuns = activeCount

	// 4. lastSuccessTime 갱신
	for _, record := range cjs.Status.ExecutionHistory {
		if record.Phase == "Succeeded" && record.CompletionTime != nil {
			if cjs.Status.LastSuccessTime == nil || record.CompletionTime.After(cjs.Status.LastSuccessTime.Time) {
				cjs.Status.LastSuccessTime = record.CompletionTime
			}
		}
	}

	// 5. 다음 실행 시각 계산
	nextTime, err := getNextScheduleTime(cjs.Spec.Schedule, cjs.Status.LastScheduleTime)
	if err != nil {
		log.Error(err, "invalid cron schedule", "schedule", cjs.Spec.Schedule)
		return ctrl.Result{}, err
	}

	// 6. 아직 실행 시각이 아니면 대기
	if time.Now().Before(nextTime) {
		if err := r.Status().Update(ctx, cjs); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Until(nextTime)}, nil
	}

	// 7. ConcurrencyPolicy 체크
	if cjs.Status.ActiveRuns > 0 {
		switch cjs.Spec.ConcurrencyPolicy {
		case cronv1.ForbidConcurrent:
			log.Info("skipping new run: previous run still active", "activeRuns", cjs.Status.ActiveRuns)
			next, _ := getNextScheduleTime(cjs.Spec.Schedule, &metav1.Time{Time: nextTime})
			if err := r.Status().Update(ctx, cjs); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: time.Until(next)}, nil
		case cronv1.ReplaceConcurrent:
			log.Info("replacing active runs", "activeRuns", cjs.Status.ActiveRuns)
			if err := r.cancelActiveRuns(ctx, cjs); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// 8. 새 run 시작
	runID := generateRunID(cjs.Name)
	log.Info("triggering new run", "runID", runID)

	if err := r.executeDag(ctx, cjs, runID); err != nil {
		return ctrl.Result{}, err
	}

	// 9. lastScheduleTime 기록
	now := metav1.Now()
	cjs.Status.LastScheduleTime = &now

	// 10. 이력 정리
	trimHistory(cjs)

	// 11. Status 업데이트
	if err := r.Status().Update(ctx, cjs); err != nil {
		return ctrl.Result{}, err
	}

	// 12. 다음 실행까지 대기
	next, _ := getNextScheduleTime(cjs.Spec.Schedule, &now)
	return ctrl.Result{RequeueAfter: time.Until(next)}, nil
}

// getNextScheduleTime은 cron 표현식과 마지막 실행 시각을 받아 다음 실행 시각을 반환한다.
// lastScheduleTime이 nil이면(한 번도 실행 안 됨) 1시간 전부터 계산해서
// Operator 시작 직후 즉시 첫 실행이 트리거되도록 한다.
func getNextScheduleTime(schedule string, last *metav1.Time) (time.Time, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := parser.Parse(schedule)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron 표현식 파싱 실패: %w", err)
	}
	from := time.Now().Add(-time.Hour)
	if last != nil {
		from = last.Time
	}
	return sched.Next(from), nil
}

// trimHistory는 ExecutionHistory를 historyLimit 개수만큼만 유지한다.
// 오래된 것부터 제거하고, Job은 OwnerReference GC에 맡긴다.
func trimHistory(cjs *cronv1.CronJobSchedule) {
	limit := cjs.Spec.HistoryLimit
	if limit <= 0 {
		limit = 5
	}
	history := cjs.Status.ExecutionHistory
	if int32(len(history)) > limit {
		cjs.Status.ExecutionHistory = history[int32(len(history))-limit:]
	}
}

// generateRunID는 CronJobSchedule 이름과 Unix timestamp를 조합해 유일한 run ID를 생성한다.
func generateRunID(name string) string {
	return fmt.Sprintf("%s-%d", name, time.Now().Unix())
}

// cancelActiveRuns는 Replace 정책일 때 실행 중인 run들의 Job을 전부 삭제하고 Failed로 표시한다.
func (r *CronJobScheduleReconciler) cancelActiveRuns(ctx context.Context, cjs *cronv1.CronJobSchedule) error {
	for i := range cjs.Status.ExecutionHistory {
		record := &cjs.Status.ExecutionHistory[i]
		if record.Phase != "Running" {
			continue
		}
		for j := range record.TaskStatuses {
			ts := &record.TaskStatuses[j]
			if ts.Phase == "Running" && ts.JobName != "" {
				job := &batchv1.Job{}
				if err := r.Get(ctx, types.NamespacedName{Name: ts.JobName, Namespace: cjs.Namespace}, job); err == nil {
					if err := r.Delete(ctx, job); client.IgnoreNotFound(err) != nil {
						return err
					}
				}
				ts.Phase = "Failed"
			}
		}
		now := metav1.Now()
		record.Phase = "Failed"
		record.CompletionTime = &now
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CronJobScheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cronv1.CronJobSchedule{}).
		Owns(&batchv1.Job{}).
		Named("cronjobschedule").
		Complete(r)
}
