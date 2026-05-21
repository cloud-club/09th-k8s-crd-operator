# Week 2 - 실습

> 이론 내용은 [week2-controller-reconciliation.md](./week2-controller-reconciliation.md)를 먼저 읽고 진행하는 것을 권장한다.

---

## 사전 준비

```bash
# Go 프로젝트 초기화
mkdir k8s-watch-practice && cd k8s-watch-practice
go mod init k8s-watch-practice

# client-go 설치
go get k8s.io/client-go@latest
go get k8s.io/apimachinery@latest
go get k8s.io/api@latest
```

---

## 실습 1. Controller 패턴 기본 체험 (kubectl)

### 1-1. Watch로 이벤트 흐름 체감

**터미널 1**: Watch 모드 시작

```bash
kubectl get pods -w
```

Watch는 주기적 Polling이 아니라 **이벤트 기반**임을 확인한다.  
Pod가 없을 때는 아무 출력도 없다. 이벤트가 발생할 때만 행이 추가된다.

---

### 1-2. Deployment 배포

**터미널 2**: Deployment 생성

```bash
kubectl apply -f https://k8s.io/examples/controllers/nginx-deployment.yaml
```

**터미널 1 관찰**: Pod가 `Pending → ContainerCreating → Running`으로 전환되는 과정에서  
각 상태 변화가 Watch 이벤트(MODIFIED)로 수신되어 표시되는 것을 확인한다.

---

### 1-3. Desired State 변경 (scale)

```bash
kubectl scale deployment nginx-deployment --replicas=5
```

Controller가 새로운 Desired State(5)와 Current State(3)의 차이를 감지하고  
Pod 2개를 추가 생성하는 과정을 터미널 1에서 확인한다.

---

### 1-4. Current State를 깨보기 (Pod 강제 삭제)

```bash
# Pod 이름 확인
kubectl get pods

# 특정 Pod 강제 삭제
kubectl delete pod <pod-name>
```

삭제 직후 Deployment Controller가 Desired State(5)를 유지하기 위해  
새 Pod를 자동으로 생성하는 것을 확인한다.

---

## 실습 2. client-go 활용

### 2-1. Watch 이벤트 로거 구현

`client-go`를 직접 사용해 Watch API를 호출하고, 이벤트 타입(`ADDED` / `MODIFIED` / `DELETED`)을 출력하는 프로그램을 구현한다.

`**main.go**`

```go
package main

import (
    "context"
    "fmt"
    "os"
    "path/filepath"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/tools/clientcmd"
    "k8s.io/client-go/util/homedir"
)

func main() {
    kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
    config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
    if err != nil {
        fmt.Fprintf(os.Stderr, "kubeconfig 로드 실패: %v\n", err)
        os.Exit(1)
    }

    clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        fmt.Fprintf(os.Stderr, "clientset 생성 실패: %v\n", err)
        os.Exit(1)
    }

    ctx := context.Background()

    // 1. 현재 상태 List (resourceVersion 획득)
    podList, err := clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{})
    if err != nil {
        fmt.Fprintf(os.Stderr, "List 실패: %v\n", err)
        os.Exit(1)
    }
    fmt.Printf("현재 Pod 수: %d, resourceVersion: %s\n",
        len(podList.Items), podList.ResourceVersion)

    // 2. 해당 resourceVersion 이후의 이벤트 Watch
    watcher, err := clientset.CoreV1().Pods("default").Watch(ctx, metav1.ListOptions{
        ResourceVersion:     podList.ResourceVersion,
        AllowWatchBookmarks: true,
    })
    if err != nil {
        fmt.Fprintf(os.Stderr, "Watch 실패: %v\n", err)
        os.Exit(1)
    }
    defer watcher.Stop()

    fmt.Println("Watch 시작 (Ctrl+C로 종료)...")

    // 3. 이벤트 수신 루프
    for event := range watcher.ResultChan() {
        switch event.Type {
        case "ADDED":
            obj := event.Object.(metav1.Object)
            fmt.Printf("[ADDED]    %s\n", obj.GetName())
        case "MODIFIED":
            obj := event.Object.(metav1.Object)
            fmt.Printf("[MODIFIED] %s\n", obj.GetName())
        case "DELETED":
            obj := event.Object.(metav1.Object)
            fmt.Printf("[DELETED]  %s\n", obj.GetName())
        case "BOOKMARK":
            fmt.Printf("[BOOKMARK] resourceVersion 갱신\n")
        case "ERROR":
            fmt.Printf("[ERROR]    %v\n", event.Object)
        }
    }
}
```

```bash
go run main.go
# 출력 예시:
# 현재 Pod 수: 3, resourceVersion: 84932
# Watch 시작 (Ctrl+C로 종료)...
# [ADDED]    nginx-deployment-5d59d67564-xkv9p
# [MODIFIED] nginx-deployment-5d59d67564-xkv9p
# [DELETED]  nginx-deployment-5d59d67564-xkv9p
```

---

### 2-2. Desired State와 Current State 비교 로그

Deployment의 `spec.replicas`(Desired)와 `status.readyReplicas`(Current)를 비교해 상태를 출력한다.

```go
package main

import (
    "context"
    "fmt"
    "os"
    "path/filepath"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/tools/clientcmd"
    "k8s.io/client-go/util/homedir"
)

func main() {
    kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
    config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
    if err != nil {
        fmt.Fprintf(os.Stderr, "kubeconfig 로드 실패: %v\n", err)
        os.Exit(1)
    }

    clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        fmt.Fprintf(os.Stderr, "clientset 생성 실패: %v\n", err)
        os.Exit(1)
    }

    ctx := context.Background()
    namespace := "default"

    deployments, err := clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
    if err != nil {
        fmt.Fprintf(os.Stderr, "Deployment 조회 실패: %v\n", err)
        os.Exit(1)
    }

    for _, d := range deployments.Items {
        desiredReplicas := int32(1)
        if d.Spec.Replicas != nil {
            desiredReplicas = *d.Spec.Replicas
        }
        currentReplicas := d.Status.ReadyReplicas

        fmt.Printf("=== Deployment: %s ===\n", d.Name)
        fmt.Printf("  Desired replicas:  %d\n", desiredReplicas)
        fmt.Printf("  Current replicas:  %d\n", currentReplicas)

        if currentReplicas < desiredReplicas {
            fmt.Printf("  상태: [부족] Pod %d개 생성 필요\n", desiredReplicas-currentReplicas)
        } else if currentReplicas > desiredReplicas {
            fmt.Printf("  상태: [초과] Pod %d개 삭제 필요\n", currentReplicas-desiredReplicas)
        } else {
            fmt.Printf("  상태: [정상] Desired == Current\n")
        }
        fmt.Println()
    }
}
```

```bash
go run main.go
# 출력 예시:
# === Deployment: nginx-deployment ===
#   Desired replicas:  3
#   Current replicas:  2
#   상태: [부족] Pod 1개 생성 필요
```

---

## 심화 실습 추천

### 심화 1. Informer + Workqueue 기반 미니 Controller 직접 구현

`client-go`의 공식 레퍼런스 구현체인 `[kubernetes/sample-controller](https://github.com/kubernetes/sample-controller)`를 분석하거나,  
아래 흐름을 직접 구현해보자.

```
목표: Pod가 삭제되면 자동으로 재생성하는 간단한 Controller

구현 순서:
1. SharedInformerFactory로 Pod Informer 생성
2. EventHandler(OnDelete)에서 Workqueue에 키 추가
3. Reconcile 함수: Lister로 Pod 조회 → 없으면 재생성
4. WaitForCacheSync 후 워커 고루틴 시작

학습 포인트:
  - Informer/Lister 연결 방식
  - Workqueue Add vs AddRateLimited 차이
  - Reconcile의 멱등성 구현
```

---

### 심화 2. Finalizer 동작 실험

Finalizer를 가진 리소스를 삭제하면 실제로 삭제가 지연되는 것을 직접 확인한다.

```bash
# Finalizer가 있는 ConfigMap 생성
kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: finalizer-test
  finalizers:
    - test.example.com/protect
data:
  key: value
EOF

# 삭제 시도 (즉시 삭제 안 됨)
kubectl delete configmap finalizer-test

# 다른 터미널에서 확인: deletionTimestamp가 설정되었지만 오브젝트는 여전히 존재
kubectl get configmap finalizer-test -o yaml | grep deletionTimestamp

# Finalizer 수동 제거 → 실제 삭제 진행
kubectl patch configmap finalizer-test \
  --type=json \
  -p='[{"op":"remove","path":"/metadata/finalizers/0"}]'
```

---

### 심화 3. Owner Reference와 가비지 컬렉션 확인

부모 리소스가 삭제될 때 ownerReference로 연결된 자식 리소스가 자동 삭제되는 것을 확인한다.

```bash
# 1. Deployment 생성 후 소유한 Pod 이름과 ownerReference 확인
kubectl get pods -o=custom-columns=NAME:.metadata.name,OWNER:.metadata.ownerReferences[0].name

# 2. Deployment 삭제 → ReplicaSet → Pod 순으로 연쇄 삭제 확인
kubectl delete deployment nginx-deployment
kubectl get pods -w   # Pod들이 Terminating → 삭제됨 확인
```

---

### 심화 4. kubectl --v=8로 Watch HTTP 스트림 원문 확인

```bash
# HTTP 레벨의 Watch 요청과 청크 스트리밍 원문 확인
kubectl get pods -w --v=8 2>&1 | grep -E "(GET|WATCH|Response|{\"type)"
```

출력에서 아래를 직접 확인한다:

- `GET /api/v1/namespaces/default/pods?watch=true` 요청
- `Transfer-Encoding: chunked` 헤더
- `{"type":"ADDED",...}` 청크 스트림

---

### 심화 5. sample-controller 코드 분석

공식 `kubernetes/sample-controller` 저장소는  
Informer + Workqueue + Reconciler 패턴의 **공식 레퍼런스 구현체**다.

```bash
git clone https://github.com/kubernetes/sample-controller
```

분석 순서 추천:

1. `main.go` → kubeconfig, clientset, informer factory 초기화 방식
2. `controller.go` → `NewController()`: EventHandler와 Workqueue 연결 방식
3. `controller.go` → `Run()`: goroutine으로 워커 실행
4. `controller.go` → `processNextWorkItem()`: Workqueue → Reconcile 호출 흐름
5. `controller.go` → `syncHandler()`: 실제 Reconcile 로직 (Desired vs Current 비교)

