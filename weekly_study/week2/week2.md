# 09th-k8s-crd-operator
1. k8s 상태 유지 원리 한 줄 요약
사용자 선언
  ↓
YAML / kubectl apply
  ↓
API Server
  ↓
etcd에 원하는 상태 저장
  ↓
Controller가 상태 감시
  ↓
현재 상태와 원하는 상태 비교
  ↓
차이가 있으면 조정
  ↓
원하는 상태에 가까워짐

- kubectl apply 기반 선언형 관리를 configuration file에 정의된 객체를 생성/업데이트하는 방식이다. 


2. 명령형 vs 선언형 설계 철학
2-1. 명령형 방식
- 명령형 방식은 사용자가 해야 할 동작을 직접 지시하는 방식이다. 
- 예시
    kubectl run nginx --image=nginx //nginx Pod 하나 만들어줘
    kubectl scale deployment nginx --replicas=3 //replica를 3개로 늘려줘
    kubectl delete pod nginx-xxx //이 Pod 삭제해줘

- 특징
| 항목    | 설명                                               |
| ----- | ------------------------------------------------ |
| 중심 개념 | 동작                                               |
| 사용 방식 | 명령어를 직접 실행                                       |
| 장점    | 빠르게 테스트하기 좋음                                     |
| 단점    | 최종 상태를 파일로 추적하기 어려움                              |
| 예시    | `kubectl run`, `kubectl create`, `kubectl scale` |

- 문제점
    - 누가 어떤 명령어를 입력했는지 모른다.
    - 현재 클러스터 상태가 왜 이렇게 됐는지 추적하기 어렵다.
    - Git으로 관리하기 어렵다.
    - 장애 복구 시 동일한 환경 재현이 어렵다.
2-2. 선언형 방식
- 선언형 방식은 사용자가 원하는 최종 상태를 YAML파일에 적어두는 방식이다.
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
        - name: nginx
          image: nginx:1.25

위의 코드의 적용 코드
- kubectl apply -f {리소스 이름}


3. Reconcile의 본질
- 시스템의 현재 상태(Current State)를 사용자가 원하는 바라는 상태(Desired State)로 일치시키기 위해 끊임없이 작동하는 무한 루프(지속적인 제어 루프) 메커니즘이다.
- Reconcile은 쉽게 말하면 원하는 상태와 현재 상태의 차이를 줄이는 과정이다.
- k8s의 핵심 철학인 '선언형(Declarative) 패키지 모델'을 유지하는 심장과 같은 기능입니다.
- 

예시: Deployment에 밑과 같은 선언형 명령어를 작성했다고 치면..

spec:
  replicas: 3

그런데 실제 Pod는 1개만 떠 있다.

원하는 상태: Pod 3개
현재 상태: Pod 1개
차이: Pod 2개 부족
조치: Pod 2개 추가 생성

이게 Reconcile이다.

3-1. Reconcile Loop
- 쿠버네티스의 컨트롤러(Controller)들은 백그라운드에서 아래의 3단계 과정을 끊임없이 반복(Reconciliation Loop)한다.

[관찰 (Observe)] ──> [비교 (Analyze)] ──> [조정 (Act)] 
      ▲                                         │
      └─────────────────────────────────────────┘

1. 관찰 (Observe): 실제 클러스터에서 실행 중인 리소스의 현재 상태를 실시간으로 모니터링한다.
2. 비교 (Analyze): 사용자가 YAML 파일(kubectl apply)을 통해 선언한 바라는 상태와 현재 상태의 차이점(Diff)을 분석한다.
3. 조정 (Act): 차이점이 있다면, 현재 상태를 바라는 상태와 똑같이 만들기 위한 변경 작업을 수행한다.

3-2. 개발 관점에서의 Reconcile

func (r *MyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. API 서버에서 사용자가 요청한 리소스 정보를 가져옴 (Observe)
    instance := &myv1.MyResource{}
    err := r.Get(ctx, req.NamespacedName, instance)

    // 2. 현재 클러스터에 배포된 실제 리소스(예: Pod, Service) 상태 확인 (Observe)
    actualPod := &corev1.Pod{}
    err = r.Get(ctx, types.NamespacedName{...}, actualPod)

    // err: 작업 중 에러 발생. k8s가 자동으로 백오프(Backoff) 알고리즘에 따라 잠시 후 재시도.

    // 3. 바라는 상태와 현재 상태 비교 후 행동 (Analyze & Act)
    if actualPod이_없다면 {
        return ctrl.Result{}, r.Create(ctx, 새로운_Pod_생성) // 조정 수행
    }

    // 4. 상태가 일치하면 아무 작업도 하지 않고 대기 (or 주기적 재확인 예약)
    return ctrl.Result{}, nil
}
