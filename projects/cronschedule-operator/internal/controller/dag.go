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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cronv1 "github.com/cloud-club/09th-k8s-crd-operator/projects/cronschedule-operator/api/v1"
)

// dependency 체크는 모든 task마다 반복적으로 일어나는데, 매번 TaskStatus 슬라이스를
// 처음부터 순회(O(n))하면 task가 많아질수록 느려진다. 한 번 map으로 변환해두면
// 이후의 모든 조회가 O(1)이 된다.
func buildTaskPhaseMap(statuses []cronv1.TaskStatus) map[string]string {
	m := make(map[string]string, len(statuses))
	for _, s := range statuses {
		m[s.Name] = s.Phase
	}
	return m
}

// getTaskDecision은 한 task의 의존성 상태를 보고 다음에 뭘 해야 하는지 결정한다.
func getTaskDecision(task cronv1.Task, phaseMap map[string]string) string {
	for _, dep := range task.Dependencies {
		phase := phaseMap[dep]
		if phase == "Failed" || phase == "Skipped" {
			return "Skipped"
		}
		if phase != "Succeeded" {
			return "Wait"
		}
	}
	return "Run"
}

// findRecordByRunID는 ExecutionHistory에서 현재 처리 중인 run(runID)의 기록을 찾는다.
// 못 찾으면 nil을 반환하는데, 이는 "아직 시작 안 한 새 run"이라는 신호로 쓰인다
func findRecordByRunID(history []cronv1.ExecutionRecord, runID string) *cronv1.ExecutionRecord {
	for i := range history {
		if history[i].RunID == runID {
			return &history[i]
		}
	}
	return nil
}

// findTaskSpec은 task 이름으로 spec(image, command, dependencies 등)을 찾는다.
// TaskStatus에는 이름과 진행 상태만 들어있고 image/command 같은 실행 정보는 없기
// 때문에, Job을 만들려면 spec.tasks[]에서 원본 정의를 다시 찾아와야 한다.
func findTaskSpec(tasks []cronv1.Task, name string) *cronv1.Task {
	for i := range tasks {
		if tasks[i].Name == name {
			return &tasks[i]
		}
	}
	return nil
}

// updateRunPhase는 run 전체의 최종 상태(Phase)를 결정한다.
// 1) 아직 Running/Pending인 task가 하나라도 있으면 run은 계속 진행 중이므로 아무것도
//    하지 않고 반환한다.
// 2) 모든 task가 끝난(Succeeded/Failed/Skipped) 상태라면 run도 끝난 것 → 완료 시각을
//    찍고, Failed가 하나라도 있으면 run 전체를 Failed로, 없으면 Succeeded로 확정한다.
func updateRunPhase(record *cronv1.ExecutionRecord) {
	for _, ts := range record.TaskStatuses {
		if ts.Phase == "Running" || ts.Phase == "Pending" {
			return
		}
	}
	now := metav1.Now()
	record.CompletionTime = &now
	for _, ts := range record.TaskStatuses {
		if ts.Phase == "Failed" {
			record.Phase = "Failed"
			return
		}
	}
	record.Phase = "Succeeded"
}

// createJobForTask는 task 하나당 K8s Job 하나를 만든다. (Airflow KubernetesExecutor가
// 태스크마다 별도 Pod를 띄우는 것과 동일한 패턴 — 태스크 간 의존성 충돌/리소스 격리를
// Job/Pod 단위로 해결한다.)
//
// ctrl.SetControllerReference로 OwnerReference를 걸어두면, CronJobSchedule이 삭제될 때
// 이 Job도 K8s GC가 자동으로 같이 삭제해준다(cascade delete) — 컨트롤러가 직접 자식
// 리소스를 일일이 정리할 필요가 없어진다.
func (r *CronJobScheduleReconciler) createJobForTask(
	ctx context.Context, cjs *cronv1.CronJobSchedule, task cronv1.Task, jobName, runID string,
) error {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: cjs.Namespace,
			Labels: map[string]string{
				"cronjobschedule": cjs.Name,
				"run-id":          runID,
				"task":            task.Name,
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					// 태스크 Pod는 1회성 작업이므로 재시작하지 않는다.
					// 재시도는 Job 레벨이 아니라 syncRunningTasks가 새 이름으로 Job을
					// 다시 만드는 방식으로 처리한다.
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    task.Name,
							Image:   task.Image,
							Command: task.Command,
							Args:    task.Args,
						},
					},
				},
			},
		},
	}
	if err := ctrl.SetControllerReference(cjs, job, r.Scheme); err != nil {
		return err
	}
	return r.Create(ctx, job)
}

// syncRunningTasks는 현재 Running으로 표시된 task들의 "실제" Job 상태를 K8s API에서
// 다시 읽어와 phase를 갱신한다. Argo Workflows 컨트롤러가 자신이 띄운 Pod의 상태를
// Watch해서 워크플로 상태를 갱신하는 것과 정확히 같은 역할.
//
// Job이 실패하면 곧바로 Failed로 확정하지 않고, task.Retries만큼 남았으면 새 이름의
// Job을 다시 만들어 재시도한다(phase는 Running 유지). 재시도를 다 쓴 뒤에야 Failed로
// 확정한다.
func (r *CronJobScheduleReconciler) syncRunningTasks(
	ctx context.Context, cjs *cronv1.CronJobSchedule,
	record *cronv1.ExecutionRecord, phaseMap map[string]string, runID string,
) error {
	for i := range record.TaskStatuses {
		ts := &record.TaskStatuses[i]
		if ts.Phase != "Running" {
			continue
		}

		job := &batchv1.Job{}
		if err := r.Get(ctx, types.NamespacedName{Name: ts.JobName, Namespace: cjs.Namespace}, job); err != nil {
			// Job이 아직 API 서버에 안 보이는 경우(생성 직후 캐시 지연 등)는 다음
			// reconcile에서 다시 확인하면 되므로 에러로 취급하지 않는다.
			if client.IgnoreNotFound(err) != nil {
				return err
			}
			continue
		}

		if job.Status.Succeeded > 0 {
			ts.Phase = "Succeeded"
			phaseMap[ts.Name] = "Succeeded"
			continue
		}

		if job.Status.Failed == 0 {
			// Pod가 아직 실행 중 → phase를 Running으로 유지하고 다음 호출에서 다시 확인한다.
			continue
		}

		taskSpec := findTaskSpec(cjs.Spec.Tasks, ts.Name)
		if taskSpec != nil && ts.RetryCount < taskSpec.Retries {
			ts.RetryCount++
			retryJobName := fmt.Sprintf("%s-%s-retry-%d", runID, ts.Name, ts.RetryCount)
			if err := r.createJobForTask(ctx, cjs, *taskSpec, retryJobName, runID); err != nil {
				if !apierrors.IsAlreadyExists(err) {
					return err
				}
			}
			ts.JobName = retryJobName
			// 재시도 중이므로 phase는 Running 유지, phaseMap도 그대로 둔다.
			continue
		}

		ts.Phase = "Failed"
		phaseMap[ts.Name] = "Failed"
	}
	return nil
}

// executeDag는 DAG 한 번의 실행(run)을 시작하거나 한 단계 진행시키는 진입점이다.
func (r *CronJobScheduleReconciler) executeDag(
	ctx context.Context, cjs *cronv1.CronJobSchedule, runID string,
) error {
	// 1. 이 run을 처음 보는 거라면 ExecutionRecord를 새로 만들고, 모든 task를
	//    Pending으로 초기화한다.
	record := findRecordByRunID(cjs.Status.ExecutionHistory, runID)
	if record == nil {
		now := metav1.Now()
		cjs.Status.ExecutionHistory = append(cjs.Status.ExecutionHistory, cronv1.ExecutionRecord{
			RunID:     runID,
			StartTime: now,
			Phase:     "Running",
		})
		record = &cjs.Status.ExecutionHistory[len(cjs.Status.ExecutionHistory)-1]
		for _, task := range cjs.Spec.Tasks {
			record.TaskStatuses = append(record.TaskStatuses, cronv1.TaskStatus{
				Name:  task.Name,
				Phase: "Pending",
			})
		}
	}

	// 2. dependency 조회를 O(1)로 만들기 위한 맵
	phaseMap := buildTaskPhaseMap(record.TaskStatuses)

	// 3. 이미 떠 있는 Job들의 실제 완료/실패 여부를 먼저 반영한다.
	if err := r.syncRunningTasks(ctx, cjs, record, phaseMap, runID); err != nil {
		return err
	}

	// 4. 아직 시작 안 한(Pending) task들을 순회하며 실행 가능 여부를 판단한다.
	for i := range record.TaskStatuses {
		ts := &record.TaskStatuses[i]
		if ts.Phase != "Pending" {
			continue
		}
		taskSpec := findTaskSpec(cjs.Spec.Tasks, ts.Name)
		if taskSpec == nil {
			continue
		}
		switch getTaskDecision(*taskSpec, phaseMap) {
		case "Run":
			jobName := fmt.Sprintf("%s-%s", runID, ts.Name)
			if err := r.createJobForTask(ctx, cjs, *taskSpec, jobName, runID); err != nil {
				// 같은 Job이 이미 만들어져 있다면(이전 reconcile에서 생성됐는데
				// status 갱신만 누락된 경우) 에러로 보지 않고 그냥 넘어간다.
				if !apierrors.IsAlreadyExists(err) {
					return err
				}
			}
			ts.Phase = "Running"
			ts.JobName = jobName
			phaseMap[ts.Name] = "Running"
		case "Skipped":
			ts.Phase = "Skipped"
			phaseMap[ts.Name] = "Skipped"
		}
		// "Wait"인 경우는 아무것도 하지 않고 다음 reconcile에서 다시 평가한다.
	}

	// 5. 모든 task가 끝났다면 run 전체의 Phase를 Succeeded/Failed로 확정한다.
	updateRunPhase(record)
	return nil
}

// syncActiveRuns는 현재 "Running" 상태인 모든 run을 한 번에 진행시킨다.
// Job 상태가 바뀌면 Owns(&batchv1.Job{}) 설정 덕분에 Reconcile이 다시 호출되는데,
// 그 시점에 어떤 run이 진행 중이었는지 일일이 추적할 필요 없이 ExecutionHistory를
// 보고 Running인 run들을 모두 executeDag로 한 단계씩 진행시키면 된다.
func (r *CronJobScheduleReconciler) syncActiveRuns(
	ctx context.Context, cjs *cronv1.CronJobSchedule,
) error {
	for i := range cjs.Status.ExecutionHistory {
		if cjs.Status.ExecutionHistory[i].Phase != "Running" {
			continue
		}
		runID := cjs.Status.ExecutionHistory[i].RunID
		if err := r.executeDag(ctx, cjs, runID); err != nil {
			return err
		}
	}
	return nil
}
