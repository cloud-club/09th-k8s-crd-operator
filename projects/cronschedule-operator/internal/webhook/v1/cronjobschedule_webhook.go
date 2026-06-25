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

package v1

import (
	"context"
	"fmt"

	"github.com/robfig/cron/v3"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	cronv1 "github.com/cloud-club/09th-k8s-crd-operator/projects/cronschedule-operator/api/v1"
)

// nolint:unused
// log is for logging in this package.
var cronjobschedulelog = logf.Log.WithName("cronjobschedule-resource")

// SetupCronJobScheduleWebhookWithManager registers the webhook for CronJobSchedule in the manager.
//
// 이 프로젝트가 고정한 controller-runtime(v0.21.0)에는 kubebuilder 최신 CLI가 기본
// 생성하는 제네릭 버전 빌더(NewWebhookManagedBy(mgr, obj))가 없어서, 의존성을 올리는
// 대신 이 버전에 있는 비-제네릭 빌더(.For(obj))를 그대로 쓴다.
func SetupCronJobScheduleWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&cronv1.CronJobSchedule{}).
		WithValidator(&CronJobScheduleCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-cron-example-com-v1-cronjobschedule,mutating=false,failurePolicy=fail,sideEffects=None,groups=cron.example.com,resources=cronjobschedules,verbs=create;update,versions=v1,name=vcronjobschedule-v1.kb.io,admissionReviewVersions=v1

// CronJobScheduleCustomValidator struct is responsible for validating the CronJobSchedule resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type CronJobScheduleCustomValidator struct{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type CronJobSchedule.
func (v *CronJobScheduleCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	cjs, ok := obj.(*cronv1.CronJobSchedule)
	if !ok {
		return nil, fmt.Errorf("expected a CronJobSchedule object but got %T", obj)
	}
	cronjobschedulelog.Info("Validation for CronJobSchedule upon creation", "name", cjs.GetName())
	return nil, validateCronJobSchedule(cjs)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type CronJobSchedule.
func (v *CronJobScheduleCustomValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	cjs, ok := newObj.(*cronv1.CronJobSchedule)
	if !ok {
		return nil, fmt.Errorf("expected a CronJobSchedule object but got %T", newObj)
	}
	cronjobschedulelog.Info("Validation for CronJobSchedule upon update", "name", cjs.GetName())
	return nil, validateCronJobSchedule(cjs)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type CronJobSchedule.
// 삭제는 spec 검증 대상이 아니므로 그냥 통과시킨다.
func (v *CronJobScheduleCustomValidator) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	cronjobschedulelog.Info("Validation for CronJobSchedule upon deletion", "name", obj.(*cronv1.CronJobSchedule).GetName())
	return nil, nil
}

// validateCronJobSchedule은 admission 단계에서 spec을 검증한다. 여기서 걸러내는 이유:
// 이 4가지 문제는 컨트롤러가 런타임에 만나면 "조용한 실패"로만 드러난다 — 잘못된 cron
// 표현식은 Reconcile이 매번 에러를 내며 무한 재시도하고, 순환 참조나 존재하지 않는
// dependency는 task들이 영원히 Wait 상태로 멈춘다(에러도 안 남). admission에서 막으면
// 그런 상태 자체가 클러스터에 생기지 않는다.
func validateCronJobSchedule(cjs *cronv1.CronJobSchedule) error {
	if err := validateSchedule(cjs.Spec.Schedule); err != nil {
		return err
	}
	if err := validateTaskNames(cjs.Spec.Tasks); err != nil {
		return err
	}
	if err := validateDependencies(cjs.Spec.Tasks); err != nil {
		return err
	}
	if err := detectCycle(cjs.Spec.Tasks); err != nil {
		return err
	}
	return nil
}

// validateSchedule은 controller의 getNextScheduleTime이 쓰는 것과 동일한 파서로
// cron 표현식을 검증한다 — 여기서 통과한 표현식은 런타임에서도 반드시 파싱에 성공한다.
func validateSchedule(schedule string) error {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(schedule); err != nil {
		return fmt.Errorf("유효하지 않은 cron 표현식 %q: %w", schedule, err)
	}
	return nil
}

// validateTaskNames는 task 이름 중복을 막는다. 이름은 Job 이름(runID-taskName)과
// TaskStatus를 식별하는 키로 쓰이기 때문에, 중복되면 두 task의 상태가 한 항목으로
// 뒤섞인다.
func validateTaskNames(tasks []cronv1.Task) error {
	seen := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		if seen[t.Name] {
			return fmt.Errorf("task 이름이 중복됨: %q", t.Name)
		}
		seen[t.Name] = true
	}
	return nil
}

// validateDependencies는 존재하지 않는 task를 참조하는 dependency를 막는다.
// 이런 dependency는 getTaskDecision에서 phaseMap[dep]가 항상 빈 문자열로 조회되어
// 영원히 "Wait"로만 평가되고, 해당 task는 절대 실행되지 않는 상태로 멈춘다.
func validateDependencies(tasks []cronv1.Task) error {
	names := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		names[t.Name] = true
	}
	for _, t := range tasks {
		for _, dep := range t.Dependencies {
			if !names[dep] {
				return fmt.Errorf("task %q가 존재하지 않는 task %q에 의존함", t.Name, dep)
			}
		}
	}
	return nil
}

// detectCycle은 DFS로 task dependency 그래프에 순환이 있는지 찾는다.
// state: 0=미방문, 1=현재 DFS 경로 위, 2=방문 완료.
// 현재 경로(1) 위에 있는 노드를 다시 만난다는 건 A→B→...→A처럼 돌아왔다는 뜻 — 순환이다.
// 순환에 걸린 task들은 모든 dependency가 Succeeded인 적이 없어서 getTaskDecision이
// 영원히 "Wait"만 반환하고, run이 끝나지 않는 채로 멈춘다.
func detectCycle(tasks []cronv1.Task) error {
	graph := make(map[string][]string, len(tasks))
	for _, t := range tasks {
		graph[t.Name] = t.Dependencies
	}
	state := make(map[string]int, len(tasks))

	var dfs func(node string) bool
	dfs = func(node string) bool {
		state[node] = 1
		for _, dep := range graph[node] {
			if state[dep] == 1 {
				return true
			}
			if state[dep] == 0 && dfs(dep) {
				return true
			}
		}
		state[node] = 2
		return false
	}

	for _, t := range tasks {
		if state[t.Name] == 0 && dfs(t.Name) {
			return fmt.Errorf("task dependency에 순환 참조가 있음 (%q 포함)", t.Name)
		}
	}
	return nil
}
