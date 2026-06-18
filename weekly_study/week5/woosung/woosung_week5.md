# Kubebuilder 튜토리얼 - CronJob Controller 만들기

> 다음의 자료들을 기반으로 작성
> kubebuilder 공식문서 - CronJob 튜토리얼: https://book.kubebuilder.io/cronjob-tutorial/controller-implementation.html
> kubebuilder repository: https://github.com/kubernetes-sigs/kubebuilder

- setup: `git clone --branch <본인의 kubebuilder 버전> --depth 1 https://github.com/kubernetes-sigs/kubebuilder`

> 튜토리얼 예제코드는 `/docs/book/src/cronjob-tutorial/testdata/project` 에 위치
 
이 튜토리얼에서는 Kubernetes 빌트인 `CronJob`을 직접 커스텀 컨트롤러로 구현한다.

---

## 1. 큰 그림: 컨트롤러는 무엇을 하는가

CronJob 컨트롤러의 임무는 다음과 같다.

> **사용자가 선언한 cron 스케줄(`spec`)에 맞춰 `Job`을 생성하고,
> 오래된 Job을 정리하며, 현재 상태(`status`)를 관측된 사실로 갱신한다.**

컨트롤러는 reconciliation loop 방식으로 동작한다.
"지금 무엇이 바뀌었는가"를 추적하는 게 아니라, 매번 **actual state** 를 관찰하고 이를 **desired state** 에 맞추는 작업을 처음부터 다시 수행한다.

- **status는 매 reconcile마다 세상의 상태로부터 재구성한다.** 자기 자신의 status를 읽어서 판단하지 않는다(예외: 최적화 목적의 `LastScheduleTime`).
- 작업은 **멱등(idempotent)** 해야 한다. 같은 reconcile이 여러 번 돌아도 결과가 동일해야 한다.

---

## 2. 핵심 타입과 의존성

### `CronJobReconciler` 구조체 ([cronjob_controller.go:52](internal/controller/cronjob_controller.go#L52))

```go
type CronJobReconciler struct {
    client.Client          // 클러스터에 대한 읽기/쓰기 클라이언트
    Scheme *runtime.Scheme // Go 타입 ↔ GVK 매핑 (owner reference 생성에 사용)
    Clock                  // 현재 시간을 얻는 인터페이스 (테스트에서 시간 조작 가능)
}
```

### `Clock` 인터페이스 ([cronjob_controller.go:62-70](internal/controller/cronjob_controller.go#L62-L70))

```go
type Clock interface {
    Now() time.Time
}
```

시간에 의존하는 로직(스케줄 계산)을 테스트하기 위해 시간을 추상화했습니다.
- 운영 환경: `realClock`이 `time.Now()`를 그대로 호출.
- 테스트 환경: 가짜 clock을 주입해 "금요일 오후 5시"처럼 원하는 시점으로 점프 가능.

### 주요 import ([cronjob_controller.go:25-45](internal/controller/cronjob_controller.go#L25-L45))

| import | 용도 |
|--------|------|
| `github.com/robfig/cron` | cron 표현식(`*/1 * * * *` 등) 파싱 및 다음 실행 시각 계산 |
| `kbatch "k8s.io/api/batch/v1"` | Kubernetes `Job` 타입 |
| `apierrors` | `IsNotFound` 등 API 에러 분류 |
| `meta` | status condition 관리 헬퍼 |
| `ref` | Job에 대한 `ObjectReference` 생성 |
| `batchv1 ".../api/v1"` | 우리가 정의한 커스텀 `CronJob` 타입 |

---

## 3. RBAC 마커 ([cronjob_controller.go:90-94](internal/controller/cronjob_controller.go#L90-L94))

```go
// +kubebuilder:rbac:groups=batch.tutorial.kubebuilder.io,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch.tutorial.kubebuilder.io,resources=cronjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=batch.tutorial.kubebuilder.io,resources=cronjobs/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get
```

이 주석들은 **코드 생성용 마커**이다.
`make manifests` 실행 시 controller-gen이 이를 읽어 `config/rbac/role.yaml`을 생성한다.
컨트롤러가 CronJob과 Job을 **생성/관리**해야 하므로 양쪽 모두에 대한 권한이 필요하다.

---

## 4. `Reconcile` 함수: 7단계 흐름

([cronjob_controller.go:113](internal/controller/cronjob_controller.go#L113)).
controller-runtime이 관련 객체에 변화가 생길 때마다 이 함수를 호출하며,
`req`에는 대상 CronJob의 namespace/name이 담겨 있다.

전체 흐름은 다음과 같다.

```
1. CronJob 로드
2. 소유한 Job들을 나열하고 status 갱신
3. history limit에 따라 오래된 Job 삭제
4. suspend 여부 확인 → 멈춤
5. 다음 스케줄 시각 계산
6. 조건 충족 시 새 Job 생성
7. 다음 실행 시각에 맞춰 requeue 예약
```

### 단계 1 — CronJob 로드 ([L128-L170](internal/controller/cronjob_controller.go#L128-L170))

```go
var cronJob batchv1.CronJob
if err := r.Get(ctx, req.NamespacedName, &cronJob); err != nil {
    if apierrors.IsNotFound(err) {
        // 이미 삭제됨 → 조정 종료 (에러 아님)
        return ctrl.Result{}, nil
    }
    return ctrl.Result{}, err // 그 외 에러는 requeue
}
```

- **NotFound**는 정상 상황(삭제됨)으로 보고 조용히 종료한다.
- 그 외 에러는 일시적 문제일 수 있으므로 에러를 반환해 **requeue(재시도)** 한다.

만약 status condition이 비어 있으면 초기 `Progressing`(Unknown) condition을 세팅하고, **객체를 다시 fetch** 한다.

> **왜 re-fetch 하는가?** Kubernetes는 **낙관적 동시성 제어(optimistic concurrency)** 를 사용한다.
> api server가 status 업데이트 요청을 받으면 객체의 `resourceVersion`이 바뀐다.
> 이 때, 이전 `resourceVersion`로 업데이트를 시도하면
> `"the object has been modified; please apply your changes to the latest version"`
> 같은 충돌이 발생한다. 그래서 업데이트 후 최신 버전을 다시 읽어와야한다.

### 단계 2 — 자식 Job 나열 및 status 갱신 ([L179-L381](internal/controller/cronjob_controller.go#L179-L381))

```go
var childJobs kbatch.JobList
if err := r.List(ctx, &childJobs,
    client.InNamespace(req.Namespace),
    client.MatchingFields{jobOwnerKey: req.Name}); err != nil { ... }
```

이 CronJob이 소유한 모든 Job을 가져온다. `MatchingFields{jobOwnerKey: req.Name}`는 **인덱스 조회**다 (단계 마지막의 `SetupWithManager`에서 설정).

> **이 인덱스가 왜 필요한가?** CronJob이 많아지면 매번 namespace의 모든 Job을 필터링하는 것은 느리다. Job을 소유 컨트롤러 이름으로 로컬 캐시에 인덱싱해 빠르게 조회한다.

가져온 Job들을 세 갈래로 분류한다.

```go
var activeJobs, successfulJobs, failedJobs []*kbatch.Job
var mostRecentTime *time.Time
```

for문으로 Job들 하나씩 분류한다. 분류에는 헬퍼 함수를 사용한다.

- **`isJobFinished`** ([L240](internal/controller/cronjob_controller.go#L240)):
  Job의 status condition에 `Complete` 또는 `Failed`가 `True`면 끝난 것으로 판단.
  - condition 없음 → **active(진행 중)**
  - `JobFailed` → **failed**
  - `JobComplete` → **successful**
- **`getScheduledTimeForJob`** ([L255](internal/controller/cronjob_controller.go#L255)):
  Job 생성 시 어노테이션(`batch.tutorial.kubebuilder.io/scheduled-at`)에서 예정 시각을 파싱.
  이를 통해 **가장 최근 실행 시각(`mostRecentTime`)** 을 복원.

분류 후 status를 재구성한다.

```go
// 마지막 스케줄 시각
cronJob.Status.LastScheduleTime = &metav1.Time{Time: *mostRecentTime} // 또는 nil

// 진행 중인 Job들에 대한 참조 목록
cronJob.Status.Active = nil
for _, activeJob := range activeJobs {
    jobRef, _ := ref.GetReference(r.Scheme, activeJob)
    cronJob.Status.Active = append(cronJob.Status.Active, *jobRef)
}
```

그다음 **현재 상태에 맞춰 condition을 설정**한다 ([L321-L367](internal/controller/cronjob_controller.go#L321-L367)).

| 상황 | Available | 추가 condition |
|------|-----------|----------------|
| suspend됨 | `False` (Suspended) | — |
| 실패한 Job 있음 | `False` (JobsFailed) | `Degraded=True` |
| 진행 중인 Job 있음 | `True` (JobsActive) | `Progressing=True` |
| 모두 완료 | `True` (AllJobsCompleted) | `Progressing=False` |

마지막으로 **status subresource**를 통해 업데이트한다 ([L378](internal/controller/cronjob_controller.go#L378)).

```go
if err := r.Status().Update(ctx, &cronJob); err != nil { ... }
```

> 그냥 Update가 아닌 **`Status().Update`**를 쓰는 이유: status subresource에는 status에 대한 권한만 있다. 컨트롤러가 실수로 spec을 바꾸게 되는 문제를 방지한다.

### 단계 3 — 오래된 Job 정리 ([L395-L447](internal/controller/cronjob_controller.go#L395-L447))

`FailedJobsHistoryLimit`, `SuccessfulJobsHistoryLimit`에 따라 보관 개수를 초과한 오래된 Job을 삭제한다. 시작 시각(`StartTime`) 기준으로 정렬한 뒤 오래된 것부터 지운다.

```go
slices.SortStableFunc(failedJobs, func(a, b *kbatch.Job) int { ... }) // 오래된 순 정렬
for i, job := range failedJobs {
    if int32(i) >= int32(len(failedJobs)) - *cronJob.Spec.FailedJobsHistoryLimit {
        break // 최신 N개는 보존
    }
    r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground))
}
```

- 삭제는 **best-effort**다. 일부가 실패해도 그것 때문에 requeue하지 않는다.
- `DeletePropagationBackground`: Job 삭제 시 그 자식 Pod도 백그라운드로 함께 정리.
- limit이 `nil`이면(미설정) 정리를 건너뛴다.

### 단계 4 — suspend 확인 ([L456-L459](internal/controller/cronjob_controller.go#L456-L459))

```go
if cronJob.Spec.Suspend != nil && *cronJob.Spec.Suspend {
    log.V(1).Info("cronjob suspended, skipping")
    return ctrl.Result{}, nil
}
```

`spec.suspend == true`면 새 Job을 만들지 않고 종료한다.
객체를 삭제하지 않고도 **일시 정지**할 수 있어, 문제 조사나 클러스터 점검 시 유용하다.

### 단계 5 — 다음 스케줄 계산 ([L479-L555](internal/controller/cronjob_controller.go#L479-L555))

`getNextSchedule` 헬퍼가 핵심 로직이다.

```go
sched, err := cron.ParseStandard(cronJob.Spec.Schedule) // cron 표현식 파싱
```

- **시작 기준점(`earliestTime`)**: `LastScheduleTime`이 있으면 그것을, 없으면
  CronJob의 `CreationTimestamp`를 사용한다.
- **`StartingDeadlineSeconds`** 가 설정되어 있으면, 그 데드라인보다 더 과거의 실행은 무시하도록 기준점을 끌어올린다.
- 기준점부터 현재까지 모든 예정 시각을 순회하며 **놓친 마지막 실행(`lastMissed`)** 을 찾고, 현재 이후의 **다음 실행(`next`)** 을 계산한다.

```go
starts := 0
for t := sched.Next(earliestTime); !t.After(now); t = sched.Next(t) {
    lastMissed = t
    starts++
    if starts > 100 {
        return ..., fmt.Errorf("Too many missed start times (> 100) ...")
    }
}
```

> **100회 가드의 의미:** 컨트롤러가 며칠간 멈춰 있었다면 수많은 실행을 놓칠 수 있다.
> 정상이라면 모두 따라잡으면 되지만, **클럭 스큐(clock skew)나 버그**로 시작 시각이 수십 년 어긋나면 무한 루프로 CPU/메모리를 소진한다. 이를 막는 안전장치이다.

파싱 실패 등 에러 시에는 `Degraded`(InvalidSchedule) condition을 세팅하되,
**에러를 반환하지 않는다**. 스케줄이 잘못된 것은 사용자가 고쳐야 하는 문제이므로, 즉시 재시도해봐야 의미가 없다.

### 단계 6 — 새 Job 생성 ([L561-L706](internal/controller/cronjob_controller.go#L561-L706))

먼저 다음 reconcile를 예약할 결과를 미리 준비한다.

```go
scheduledResult := ctrl.Result{RequeueAfter: nextRun.Sub(r.Now())}
```

그리고 여러 가드를 통과해야 실제로 Job을 만든다.

1. **놓친 실행이 없으면** 다음 실행까지 잠들기 ([L569](internal/controller/cronjob_controller.go#L569)).
2. **데드라인 초과 검사** ([L577-L597](internal/controller/cronjob_controller.go#L577-L597)):
   놓친 실행이 `StartingDeadlineSeconds`를 넘겼다면 이번 실행은 포기하고
   `Degraded`(MissedSchedule)를 기록.
3. **동시성 정책** ([L606-L620](internal/controller/cronjob_controller.go#L606-L620)):
   - `Forbid` + 활성 Job 존재 → 이번 실행 건너뜀.
   - `Replace` → 기존 활성 Job들을 삭제하고 새로 만듦.
   - `Allow`(기본값) → 그대로 동시 실행 허용.

가드를 통과하면 `constructJobForCronJob`으로 Job 객체를 구성한다 ([L637](internal/controller/cronjob_controller.go#L637)).

```go
name := fmt.Sprintf("%s-%d", cronJob.Name, scheduledTime.Unix()) // 결정론적 이름
job := &kbatch.Job{
    ObjectMeta: metav1.ObjectMeta{ Name: name, Namespace: cronJob.Namespace, ... },
    Spec: *cronJob.Spec.JobTemplate.Spec.DeepCopy(),
}
job.Annotations[scheduledTimeAnnotation] = scheduledTime.Format(time.RFC3339)
ctrl.SetControllerReference(cronJob, job, r.Scheme) // owner reference 설정
```

여기서 세 가지가 중요하다.

- **결정론적 이름**: `<cronjob명>-<유닉스타임>`. 같은 예정 시각에 대해 같은 이름이 생성되므로,
  중복 reconcile가 발생해도 **같은 Job을 두 번 만드는 것을 방지**한다(이름 충돌 에러).
- **scheduled-at 어노테이션**: 다음 reconcile 때 `LastScheduleTime`을 복원하기 위한 표식.
- **owner reference**: 이걸 설정하면 (1) CronJob 삭제 시 GC가 Job을 자동 정리하고,
  (2) Job 변화 시 controller-runtime이 어느 CronJob을 reconcile할지 알 수 있다.

그리고 실제로 생성한다.

```go
if err := r.Create(ctx, job); err != nil { ... } // 실패 시 Degraded(JobCreationFailed)
```

성공하면 `Progressing`(JobCreated) condition을 기록한다.

### 단계 7 — requeue 예약 ([L716-L717](internal/controller/cronjob_controller.go#L716-L717))

```go
return scheduledResult, nil // RequeueAfter: 다음 실행 시각까지
```

다음 실행 시각에 맞춰 다시 깨워달라고 요청한다. 이는 **최대 데드라인**이고, 그 전에 Job이 시작/완료되거나 CronJob이 수정되면 더 일찍 reconcile된다.

---

## 5. `SetupWithManager`: 인덱스와 watch 설정 ([L740-L769](internal/controller/cronjob_controller.go#L740-L769))

```go
func (r *CronJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
    if r.Clock == nil {
        r.Clock = realClock{} // 운영용 실제 clock 주입
    }

    // (1) Job을 소유 컨트롤러 이름으로 인덱싱
    mgr.GetFieldIndexer().IndexField(context.Background(), &kbatch.Job{}, jobOwnerKey,
        func(rawObj client.Object) []string {
            job := rawObj.(*kbatch.Job)
            owner := metav1.GetControllerOf(job)
            if owner == nil { return nil }
            if owner.APIVersion != apiGVStr || owner.Kind != "CronJob" { return nil }
            return []string{owner.Name} // 이 값이 인덱스 키
        })

    // (2) 컨트롤러 등록
    return ctrl.NewControllerManagedBy(mgr).
        For(&batchv1.CronJob{}). // 주 대상: CronJob
        Owns(&kbatch.Job{}).     // 소유 대상: Job (변화 시 부모 CronJob을 reconcile)
        Named("cronjob").
        Complete(r)
}
```

두 가지를 설정한다.

1. **필드 인덱서**: 단계 2의 `MatchingFields{jobOwnerKey: req.Name}` 빠른 조회를 가능하게 하는
   `.metadata.controller` 인덱스를 만든다. Job의 owner reference에서 CronJob 이름을 추출한다.
2. **watch 등록**: `For(CronJob)`로 CronJob 변화를 감시하고, `Owns(Job)`로 이 컨트롤러가 소유한 Job의 변화(생성/삭제/완료)도 감시해 자동으로 부모 CronJob을 reconcile한다.

---

## 6. 관련 타입: `CronJobSpec`과 `CronJobStatus`

전체 정의는 [api/v1/cronjob_types.go](api/v1/cronjob_types.go)에 있다.

### `CronJobSpec` (원하는 상태)

| 필드 | 타입 | 설명 |
|------|------|------|
| `Schedule` | `string` | **필수.** cron 형식 스케줄 |
| `StartingDeadlineSeconds` | `*int64` | 놓친 실행을 시작할 수 있는 데드라인(초) |
| `ConcurrencyPolicy` | `ConcurrencyPolicy` | `Allow`(기본)/`Forbid`/`Replace` |
| `Suspend` | `*bool` | 이후 실행을 일시 정지 |
| `JobTemplate` | `batchv1.JobTemplateSpec` | **필수.** 생성할 Job의 템플릿 |
| `SuccessfulJobsHistoryLimit` | `*int32` | 성공 Job 보관 개수 |
| `FailedJobsHistoryLimit` | `*int32` | 실패 Job 보관 개수 |

> 포인터(`*int32` 등)를 쓰는 이유: **"명시적 0"과 "미설정"을 구분**하기 위해

### `CronJobStatus` (관측된 상태)

| 필드 | 설명 |
|------|------|
| `Active` | 현재 실행 중인 Job들에 대한 참조 목록 |
| `LastScheduleTime` | 마지막으로 성공적으로 스케줄된 시각 |
| `Conditions` | `Available` / `Progressing` / `Degraded` 표준 condition |

---

## 7. 핵심 설계 패턴 요약

| 패턴 | 의미 | 코드 위치 |
|------|------|-----------|
| **status 재구성** | 매 reconcile마다 세상의 상태로부터 status를 다시 만든다 | 단계 2 |
| **낙관적 동시성 / re-fetch** | status 업데이트 후 최신 객체를 다시 읽어 충돌 회피 | 곳곳의 `r.Get` |
| **결정론적 이름** | 같은 예정 시각 → 같은 Job 이름 → 중복 생성 방지 | `constructJobForCronJob` |
| **owner reference** | GC 자동 정리 + Job 변화 시 부모 reconcile | `SetControllerReference` |
| **필드 인덱스** | 소유 Job 빠른 조회 | `SetupWithManager` |
| **Clock 추상화** | 시간 의존 로직 테스트 가능 | `Clock` 인터페이스 |
| **NotFound = 정상** | 삭제된 객체는 에러 없이 종료 | 단계 1 |
| **best-effort 삭제** | 정리 실패가 reconcile 실패로 번지지 않음 | 단계 3 |
| **100회 가드** | 클럭 스큐로 인한 자원 소진 방지 | `getNextSchedule` |

---

## 8. 한 줄 요약

> `CronJobReconciler`는 매번 **소유 Job을 관찰 → status 재구성 → 오래된 Job 정리 →
> suspend/데드라인/동시성 검사 → 스케줄에 맞으면 Job 생성 → 다음 실행 시각에 requeue**
> 라는 멱등적 루프를 돌고, 사용자가 선언한 cron 스케줄을 실제 클러스터 상태로 구현한다.

---

## 추가: finalizer & GC

객체가 삭제될 때 뒷정리를 하기 위한 방식으로 GC를 활용하는 방식과 finalizer 패턴을 활용하는 방식이 있다. 서로 반대 방향의 메커니즘이다.

```
객체 삭제 시 뒷정리 누가?
   │
   ├─ 클러스터 안 자식 리소스   → owner reference + GC  (자동, 코드 거의 불필요)  ← CronJob이 쓰는 방식
   │
   └─ 클러스터 밖 외부 리소스   → finalizer            (직접 정리 로직 필요)
```

### GC — owner reference 방식

```go
ctrl.SetControllerReference(cronJob, job, r.Scheme)   // L653
```

이 한 줄이 **"이 Job의 부모는 이 CronJob이다"** 를 기록한다. 

그러면:

- CronJob을 삭제하면 → 쿠버네티스 **GC가 자식 Job들을 자동 삭제** (cascading delete)
- 컨트롤러가 Job을 직접 지우지 않아도 된다. 클러스터가 알아서 정리한다.

```
CronJob "hello" 삭제
   └─▶ GC가 owner reference 추적
        ├─▶ Job "hello-...60" 삭제
        ├─▶ Job "hello-...20" 삭제
        └─▶ (그 Job들의 Pod도 함께)
```

**핵심 한계**: owner-ref GC는 **클러스터 안 자식 리소스**만 정리 가능. CronJob 컨트롤러는 자식이 전부 클러스터 안 Job이라 **finalizer가 필요 없다.**

#### propagation policy (삭제 전파 방식)

`kubectl delete` 또는 `client.PropagationPolicy(...)`로 지정:

| 정책 | 동작 |
|------|------|
| `Background` (기본) | 부모 먼저 삭제, 자식은 백그라운드에서 비동기 정리 |
| `Foreground` | 자식 먼저 다 지우고 나서 부모 삭제 (deletionTimestamp로 대기) |
| `Orphan` | 부모만 삭제, 자식은 고아로 남김 |

CronJob 컨트롤러의 오래된 Job 정리도 `DeletePropagationBackground`를 쓴다 ([L414](internal/controller/cronjob_controller.go#L414)).

### finalizer 패턴

이 컨트롤러엔 finalizer 흔적이 **RBAC 마커 한 줄**뿐이고 실제 로직은 없다:

```go
// +kubebuilder:rbac:groups=...,resources=cronjobs/finalizers,verbs=update   // L92 (권한만)
```

`Reconcile` 안에 `DeletionTimestamp` 체크도, `AddFinalizer`/`RemoveFinalizer`도 없다.
→ finalizer는 이번 코드로는 배울 수 없는 **별도 학습 주제.**

#### 왜 필요한가?

GC가 못 지우는 것 — **클러스터 밖 리소스**가 있을 때. 예: 클라우드 로드밸런서, 외부 DNS 레코드, S3 버킷, 외부 DB row. 객체를 그냥 지우면 이 외부 자원이 **고아(orphan)** 로 남는다.

#### 동작 메커니즘 (GC와 반대 — 삭제를 붙잡아두는 역할)

```
1. 컨트롤러가 객체에 finalizer 문자열을 추가
   metadata.finalizers: ["batch.tutorial.kubebuilder.io/cleanup"]

2. 사용자가 kubectl delete
   → 쿠버네티스는 실제로 안 지움
   → 대신 metadata.deletionTimestamp 만 채우고 객체를 살려둠 (finalizer가 있으니까)

3. 컨트롤러가 reconcile에서 감지:
   if !obj.DeletionTimestamp.IsZero() {
       외부리소스_정리()
       controllerutil.RemoveFinalizer(obj, "...cleanup")
       r.Update(ctx, obj)
   }

4. finalizer 목록이 비면 → 그제서야 쿠버네티스가 객체를 진짜 삭제
```