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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	cronv1 "github.com/cloud-club/09th-k8s-crd-operator/projects/cronschedule-operator/api/v1"
)

// This spec exercises executeDag()/syncRunningTasks() directly against a real
// Kubernetes cluster (set USE_EXISTING_CLUSTER=true) so the busybox containers
// actually run on real nodes. Run with:
//
//	USE_EXISTING_CLUSTER=true go test ./internal/controller/... -v \
//	  -args -ginkgo.focus="DAG execution against a real cluster"
const dagTestNamespace = "team-3"

var _ = Describe("DAG execution against a real cluster", func() {
	var (
		cjs        *cronv1.CronJobSchedule
		runID      string
		reconciler *CronJobScheduleReconciler
	)

	BeforeEach(func() {
		reconciler = &CronJobScheduleReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}

		name := fmt.Sprintf("etl-practice-%d", time.Now().UnixNano())
		cjs = &cronv1.CronJobSchedule{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: dagTestNamespace,
			},
			Spec: cronv1.CronJobScheduleSpec{
				Schedule:          "*/5 * * * *",
				ConcurrencyPolicy: cronv1.ForbidConcurrent,
				HistoryLimit:      5,
				Tasks: []cronv1.Task{
					{
						Name:    "extract",
						Image:   "busybox",
						Command: []string{"sh", "-c", "echo extracting && sleep 5"},
					},
					{
						Name:         "transform",
						Image:        "busybox",
						Command:      []string{"sh", "-c", "echo transforming && sleep 5"},
						Dependencies: []string{"extract"},
					},
					{
						Name:         "load",
						Image:        "busybox",
						Command:      []string{"sh", "-c", "echo loading && sleep 3"},
						Dependencies: []string{"transform"},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, cjs)).To(Succeed())
		runID = fmt.Sprintf("%s-%d", cjs.Name, time.Now().Unix())
	})

	AfterEach(func() {
		Expect(k8sClient.Delete(ctx, cjs)).To(Succeed())
	})

	It("runs extract, transform, load in order as real Jobs and reaches Succeeded", func() {
		By("starting the run: extract has no dependencies, so its Job is created immediately")
		Expect(reconciler.executeDag(ctx, cjs, runID)).To(Succeed())

		extractJobName := fmt.Sprintf("%s-extract", runID)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: extractJobName, Namespace: dagTestNamespace}, &batchv1.Job{})).To(Succeed())

		By("waiting for the real Pod to finish extract, then re-running executeDag to advance the DAG")
		Eventually(func() string {
			Expect(reconciler.executeDag(ctx, cjs, runID)).To(Succeed())
			return phaseOf(cjs, runID, "extract")
		}, 2*time.Minute, 2*time.Second).Should(Equal("Succeeded"))

		Expect(phaseOf(cjs, runID, "transform")).To(Equal("Running"))

		By("waiting for transform to finish")
		Eventually(func() string {
			Expect(reconciler.executeDag(ctx, cjs, runID)).To(Succeed())
			return phaseOf(cjs, runID, "transform")
		}, 2*time.Minute, 2*time.Second).Should(Equal("Succeeded"))

		By("waiting for load to finish and the whole run to reach Succeeded")
		Eventually(func() string {
			Expect(reconciler.executeDag(ctx, cjs, runID)).To(Succeed())
			record := findRecordByRunID(cjs.Status.ExecutionHistory, runID)
			if record == nil {
				return ""
			}
			return record.Phase
		}, 2*time.Minute, 2*time.Second).Should(Equal("Succeeded"))
	})
})

// phaseOf looks up a task's phase within the in-memory ExecutionRecord for runID.
func phaseOf(cjs *cronv1.CronJobSchedule, runID, taskName string) string {
	record := findRecordByRunID(cjs.Status.ExecutionHistory, runID)
	if record == nil {
		return ""
	}
	for _, ts := range record.TaskStatuses {
		if ts.Name == taskName {
			return ts.Phase
		}
	}
	return ""
}
