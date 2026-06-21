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
	_ = logf.FromContext(ctx)

	// TODO: your logic here

	return ctrl.Result{}, nil
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

// SetupWithManager sets up the controller with the Manager.
func (r *CronJobScheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cronv1.CronJobSchedule{}).
		Owns(&batchv1.Job{}).
		Named("cronjobschedule").
		Complete(r)
}
