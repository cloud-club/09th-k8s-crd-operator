# Kubernetes API 서버 확장 — CRD (Custom Resource Definition)

> Kubernetes는 기본 제공 리소스(Pod, Deployment, Service 등) 외에도 사용자가 직접 리소스 타입을 정의할 수 있습니다.
> CRD의 개념부터 작성, 등록, Controller 구현까지 전 과정을 정리합니다.

## 목차

1. [CRD란 무엇인가](#1-crd란-무엇인가)
2. [CRD YAML 작성](#2-crd-yaml-작성)
3. [Custom Resource 생성 및 관리](#3-custom-resource-생성-및-관리)
4. [Validation — OpenAPI v3 Schema](#4-validation--openapi-v3-schema)
5. [Subresource — status와 scale](#5-subresource--status와-scale)
6. [여러 버전 관리와 Conversion Webhook](#6-여러-버전-관리와-conversion-webhook)
7. [Controller와 Operator 패턴](#7-controller와-operator-패턴)
8. [kubebuilder로 Operator 만들기](#8-kubebuilder로-operator-만들기)

## 1. CRD란 무엇인가

### Kubernetes API 서버의 확장성

Kubernetes API 서버는 리소스 타입을 런타임에 동적으로 추가할 수 있도록 설계되어 있습니다. `CustomResourceDefinition(CRD)` 리소스를 클러스터에 등록하면, Kubernetes가 해당 타입에 대한 REST API 엔드포인트를 자동으로 생성해 줍니다.

예를 들어 `example.com/v1` 그룹에 `MyApp` 타입을 정의하면 다음 API가 생성됩니다.

```
GET    /apis/example.com/v1/namespaces/{ns}/myapps
POST   /apis/example.com/v1/namespaces/{ns}/myapps
GET    /apis/example.com/v1/namespaces/{ns}/myapps/{name}
PUT    /apis/example.com/v1/namespaces/{ns}/myapps/{name}
PATCH  /apis/example.com/v1/namespaces/{ns}/myapps/{name}
DELETE /apis/example.com/v1/namespaces/{ns}/myapps/{name}
```

이 API를 통해 CRD로 정의한 리소스를 일반 Kubernetes 리소스(Pod, Deployment 등)처럼 `kubectl get`, `kubectl apply`, `kubectl delete`로 다룰 수 있습니다.

### 핵심 용어

| 용어                               | 설명                                                                     |
| ---------------------------------- | ------------------------------------------------------------------------ |
| **CRD** (CustomResourceDefinition) | "이런 타입의 리소스가 있다"고 Kubernetes에 알리는 설정 파일. 타입 정의서 |
| **CR** (Custom Resource)           | CRD로 정의된 타입의 실제 인스턴스. 사용자가 `kubectl apply`로 생성       |
| **Operator**                       | CRD + Controller. CR을 감시하고 실제 동작을 수행하는 소프트웨어 패턴     |
| **Controller**                     | CR의 현재 상태와 원하는 상태를 비교해 맞춰주는 제어 루프                 |
| **Reconcile Loop**                 | "원하는 상태(Desired State)로 수렴"하는 반복 프로세스                    |
| **Finalizer**                      | 리소스 삭제 전에 반드시 실행해야 할 정리 작업을 보장하는 메커니즘        |

### CRD 없이 불가능한 것들

CRD 이전에는 ConfigMap을 설정 저장소로 남용하거나, 직접 API 서버에 Aggregation Layer를 추가해야 했습니다. CRD 덕분에 다음이 가능해집니다.

- Kubernetes 네이티브 객체처럼 `kubectl`로 관리
- `kubectl explain`으로 스키마 문서 조회
- RBAC, Audit 로그, 접근 제어를 Kubernetes와 통합
- etcd에 데이터 저장 (별도 데이터베이스 불필요)
- Watch API로 실시간 변경 감지

## 2. CRD YAML 작성

### 전체 구조

CRD 자체도 Kubernetes 리소스입니다. `apiextensions.k8s.io/v1` 그룹에 속하며, 아래와 같이 작성합니다.

```yaml
# crd-myapp.yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  # 반드시 "<plural>.<group>" 형식이어야 함
  name: myapps.example.com
spec:
  # API 그룹 — 보통 회사 도메인을 역순으로 사용
  group: example.com

  # 스코프: 네임스페이스 내에 존재하면 Namespaced,
  # 클러스터 전체에 하나면 Cluster (Node, PersistentVolume 같은 방식)
  scope: Namespaced

  names:
    # kubectl get <plural> 로 접근
    plural: myapps
    # kubectl get myapp <name> (단수)
    singular: myapp
    # Go 구조체 이름. UpperCamelCase
    kind: MyApp
    # 짧은 이름: kubectl get ma
    shortNames:
      - ma
    # kubectl get all 에 포함할지 여부
    categories:
      - all

  versions:
    - name: v1
      # served: 이 버전으로 API 요청을 처리할지 여부
      served: true
      # storage: etcd에 저장되는 버전. 반드시 하나만 true
      storage: true

      # OpenAPI v3 스키마 — 입력값 검증에 사용
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                replicas:
                  type: integer
                  minimum: 1
                  maximum: 100
                  description: "원하는 Pod 수"
                image:
                  type: string
                  description: "컨테이너 이미지"
                config:
                  type: object
                  additionalProperties:
                    type: string
                  description: "키-값 설정 맵"
              required:
                - replicas
                - image
            status:
              type: object
              properties:
                readyReplicas:
                  type: integer
                phase:
                  type: string
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type: { type: string }
                      status: { type: string }
                      reason: { type: string }
                      message: { type: string }
                      lastTransitionTime: { type: string, format: date-time }

      # 서브리소스: status와 scale을 spec과 별도 엔드포인트로 분리
      subresources:
        status: {} # /status 엔드포인트 활성화
        scale:
          specReplicasPath: .spec.replicas
          statusReplicasPath: .status.readyReplicas

      # kubectl get 출력에 추가할 컬럼 정의
      additionalPrinterColumns:
        - name: Replicas
          type: integer
          jsonPath: .spec.replicas
        - name: Ready
          type: integer
          jsonPath: .status.readyReplicas
        - name: Phase
          type: string
          jsonPath: .status.phase
        - name: Age
          type: date
          jsonPath: .metadata.creationTimestamp
```

### CRD 등록 및 확인

```bash
# CRD를 클러스터에 등록
kubectl apply -f crd-myapp.yaml

# 등록 상태 확인
# ESTABLISHED가 True면 API 사용 가능
kubectl get crd myapps.example.com
# NAME                    CREATED AT
# myapps.example.com      2024-01-15T10:00:00Z

# 상세 정보 (조건 포함)
kubectl describe crd myapps.example.com

# 새로 생성된 API 엔드포인트 확인
kubectl api-resources | grep example.com
# NAME     SHORTNAMES  APIVERSION         NAMESPACED  KIND
# myapps   ma          example.com/v1     true        MyApp

kubectl api-versions | grep example.com
# example.com/v1

# CRD 스키마 기반 문서 확인 (explain은 CRD에도 동작함)
kubectl explain myapps
kubectl explain myapps.spec
kubectl explain myapps.spec.replicas

# CRD 삭제 (이 CRD로 생성한 CR도 모두 삭제되니 주의)
kubectl delete crd myapps.example.com
```

## 3. Custom Resource 생성 및 관리

CRD를 등록했으면, 해당 타입의 인스턴스(CR)를 생성할 수 있습니다.

### CR YAML 작성

```yaml
# cr-sample.yaml
apiVersion: example.com/v1 # CRD의 group/version
kind: MyApp # CRD의 kind
metadata:
  name: my-sample-app
  namespace: default
  # 일반 Kubernetes 리소스처럼 labels, annotations 사용 가능
  labels:
    app: my-sample-app
    team: platform
  annotations:
    example.com/managed-by: "my-operator"
    example.com/description: "샘플 애플리케이션"
spec:
  replicas: 3
  image: nginx:1.25
  config:
    env: "production"
    log-level: "info"
    max-connections: "100"
```

### 기본 CRUD 명령어

```bash
# 생성 또는 업데이트 (선언적)
kubectl apply -f cr-sample.yaml

# 조회
kubectl get myapps -n default
# NAME             REPLICAS   READY   PHASE   AGE
# my-sample-app    3          0               5s

kubectl get ma               # shortName 사용
kubectl get myapps -A        # 전체 네임스페이스
kubectl get myapps -o wide   # 추가 정보 포함
kubectl get myapps -o yaml   # 전체 YAML 출력

# 특정 필드만 추출 (JSONPath)
kubectl get myapps my-sample-app -o jsonpath='{.spec.replicas}'
kubectl get myapps my-sample-app -o jsonpath='{.spec.image}'

# 여러 필드 한번에
kubectl get myapps my-sample-app \
  -o jsonpath='{.metadata.name}: {.spec.replicas} replicas ({.spec.image})'

# 상세 정보 (이벤트 포함)
kubectl describe myapps my-sample-app -n default

# 실시간 감시
kubectl get myapps -w

# 특정 레이블로 필터링
kubectl get myapps -l team=platform
```

### 패치 방법

Kubernetes는 리소스를 부분 수정하는 여러 방법을 지원합니다.

```bash
# merge 패치: JSON merge patch — 일부 필드만 업데이트
kubectl patch myapps my-sample-app --type=merge \
  -p '{"spec":{"replicas":5}}'

# strategic merge patch (기본): 배열 병합 방식이 다름
kubectl patch myapps my-sample-app \
  -p '{"spec":{"image":"nginx:1.26"}}'

# json 패치: RFC 6902 — 정밀 제어가 필요할 때
kubectl patch myapps my-sample-app --type=json \
  -p '[{"op":"replace","path":"/spec/replicas","value":2}]'

# 인라인 편집
kubectl edit myapps my-sample-app

# status 서브리소스 업데이트 (spec과 분리되어 권한도 별도)
kubectl patch myapps my-sample-app --subresource=status --type=merge \
  -p '{"status":{"readyReplicas":3,"phase":"Running"}}'

# scale 서브리소스 사용
kubectl scale myapps my-sample-app --replicas=5
kubectl get myapps my-sample-app -o jsonpath='{.spec.replicas}'

# 삭제
kubectl delete myapps my-sample-app
kubectl delete -f cr-sample.yaml
```

## 4. Validation — OpenAPI v3 Schema

CRD에 스키마를 정의하면 잘못된 값이 들어오는 것을 API 서버 레벨에서 막을 수 있습니다. 컨트롤러가 잘못된 입력을 처리하기 전에 차단하므로 견고성이 크게 높아집니다.

### 기본 검증 필드

```yaml
# spec.versions[].schema.openAPIV3Schema.properties.spec 하위
spec:
  type: object
  # required: 반드시 포함해야 하는 필드 목록
  required: [replicas, image]
  properties:
    replicas:
      type: integer
      minimum: 1 # 최솟값
      maximum: 100 # 최댓값
      description: "원하는 Pod 수 (1~100)"

    image:
      type: string
      # pattern: 정규식으로 형식 제한
      pattern: '^[\w.\-/]+:[\w.\-]+$'
      description: "이미지:태그 형식"

    strategy:
      type: string
      # enum: 허용 값 목록
      enum: [RollingUpdate, Recreate]
      # default: 값을 지정하지 않으면 사용할 기본값
      default: RollingUpdate

    resources:
      type: object
      properties:
        cpu:
          type: string
          pattern: "^[0-9]+(m|[0-9]*)$"
          examples: ["500m", "2"]
        memory:
          type: string
          pattern: "^[0-9]+(Ki|Mi|Gi|Ti)?$"
          examples: ["256Mi", "1Gi"]

    tags:
      type: array
      # 배열 아이템 타입 및 최대 개수 제한
      maxItems: 10
      items:
        type: string
        maxLength: 63
        pattern: "^[a-z0-9][a-z0-9-]*[a-z0-9]$"

    config:
      type: object
      # 키-값 모두 string인 자유형 맵
      additionalProperties:
        type: string
      maxProperties: 20 # 최대 키 개수
```

### CEL (Common Expression Language) 검증 — Kubernetes 1.25+

단순한 타입/범위 검증을 넘어, 필드 간 관계 검증이 필요할 때 CEL을 사용합니다.

```yaml
spec:
  type: object
  x-kubernetes-validations:
    # replicas가 0인 경우 image를 지정하지 않아야 함
    - rule: "self.replicas > 0 || !has(self.image)"
      message: "replicas가 0이면 image를 지정할 수 없습니다"

    # RollingUpdate 전략이면 maxSurge를 지정해야 함
    - rule: |
        self.strategy == 'Recreate' ||
        (self.strategy == 'RollingUpdate' && has(self.maxSurge))
      message: "RollingUpdate 전략에는 maxSurge가 필요합니다"

    # 변경 불가 필드 (immutable): 생성 후 수정 금지
    - rule: "self.region == oldSelf.region"
      message: "region은 변경할 수 없습니다"
      # oldSelf: 변경 전 값 (업데이트 검증에만 사용 가능)
```

### 검증 테스트

```bash
# 올바른 CR 적용
kubectl apply -f cr-sample.yaml  # 성공

# 잘못된 값으로 테스트
cat <<EOF | kubectl apply -f -
apiVersion: example.com/v1
kind: MyApp
metadata:
  name: invalid-app
  namespace: default
spec:
  replicas: 200   # maximum: 100 위반
  image: "nginx"  # pattern 위반 (태그 없음)
EOF
# 오류 메시지:
# The MyApp "invalid-app" is invalid:
#   spec.replicas: Invalid value: 200: spec.replicas in body should be less than or equal to 100
#   spec.image: Invalid value: "nginx": spec.image in body should match '^[\w.\-/]+:[\w.\-]+$'

# 드라이런으로 미리 확인 (실제 적용 없이 서버 검증 수행)
kubectl apply -f cr-sample.yaml --dry-run=server
```

## 5. Subresource — status와 scale

### 왜 서브리소스가 필요한가?

CRD의 `spec`과 `status`를 같은 엔드포인트로 수정하면 문제가 생깁니다.

- **사용자**는 `spec`을 수정해 원하는 상태를 선언합니다.
- **컨트롤러**는 `status`를 수정해 현재 상태를 보고합니다.

같은 엔드포인트를 쓰면 두 주체가 서로의 수정을 덮어쓸 수 있습니다. `subresources.status: {}`를 활성화하면 `/status` 엔드포인트가 분리되어, 컨트롤러만 status를 수정할 수 있도록 RBAC으로 제어할 수 있습니다.

```yaml
# CRD의 versions[].subresources
subresources:
  # status 서브리소스: /apis/example.com/v1/namespaces/{ns}/myapps/{name}/status
  status: {}

  # scale 서브리소스: HPA(HorizontalPodAutoscaler)와 연동하려면 필수
  scale:
    specReplicasPath: .spec.replicas # HPA가 읽어서 scaleUp/Down
    statusReplicasPath: .status.readyReplicas # 현재 준비된 파드 수
    # labelSelectorPath: .status.selector    # 선택적: 파드 셀렉터
```

```bash
# spec 업데이트 (일반 엔드포인트)
kubectl patch myapps my-sample-app --type=merge \
  -p '{"spec":{"replicas":5}}'

# status 업데이트 (status 서브리소스 — 별도 엔드포인트)
kubectl patch myapps my-sample-app --subresource=status --type=merge -p '
{
  "status": {
    "readyReplicas": 3,
    "phase": "Running",
    "conditions": [
      {
        "type": "Available",
        "status": "True",
        "reason": "MinimumReplicasAvailable",
        "message": "Deployment has minimum availability",
        "lastTransitionTime": "2024-01-15T10:00:00Z"
      }
    ]
  }
}'

# scale 서브리소스 사용 (kubectl scale 내부적으로 이 엔드포인트 사용)
kubectl scale myapps my-sample-app --replicas=7

# HPA 연동 (scale 서브리소스 활성화 후 사용 가능)
cat <<EOF | kubectl apply -f -
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: my-sample-app-hpa
spec:
  scaleTargetRef:
    apiVersion: example.com/v1
    kind: MyApp
    name: my-sample-app
  minReplicas: 2
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
EOF
```

## 6. 여러 버전 관리와 Conversion Webhook

### 여러 버전이 필요한 이유

CRD의 스키마가 변경되면 기존 사용자에게 영향을 줍니다. Kubernetes는 여러 API 버전을 동시에 지원하는 방식으로 이 문제를 해결합니다.

```
v1alpha1 (초기 개발)
    → v1beta1 (안정화 중)
        → v1 (GA — General Availability)
```

각 버전은 동시에 `served: true`로 유지할 수 있어서, 클라이언트마다 원하는 버전을 사용할 수 있습니다. 하지만 etcd에는 하나의 버전(`storage: true`)만 저장됩니다. 다른 버전으로 요청이 오면 변환이 필요합니다.

### 버전 간 변환 (Conversion Webhook)

버전 변환 로직을 컨트롤러 외부에 Webhook으로 구현합니다.

```yaml
# CRD에 Webhook 설정 추가
spec:
  conversion:
    strategy: Webhook # None(변환 없음) 또는 Webhook
    webhook:
      clientConfig:
        service:
          name: my-operator-webhook-service
          namespace: my-system
          path: /convert
          port: 443
        # caBundle: Webhook 서버의 CA 인증서 (base64)
      conversionReviewVersions: ["v1", "v1beta1"]

  versions:
    - name: v1
      served: true
      storage: true # etcd 저장 버전
      schema: { ... }

    - name: v1beta1
      served: true # 여전히 API 제공
      storage: false # 저장은 v1으로
      schema: { ... }

    - name: v1alpha1
      served: false # API 제공 중단 (기존 CR은 v1로 마이그레이션 필요)
      storage: false
      schema: { ... }
```

```bash
# Webhook 서버 인증서 관리 — cert-manager 설치
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/latest/download/cert-manager.yaml

# cert-manager가 준비될 때까지 대기
kubectl wait --for=condition=Available deploy -l app.kubernetes.io/instance=cert-manager \
  -n cert-manager --timeout=120s

# Self-signed ClusterIssuer
cat <<EOF | kubectl apply -f -
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: selfsigned-issuer
spec:
  selfSigned: {}
EOF

# Webhook 서버용 Certificate
cat <<EOF | kubectl apply -f -
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: my-operator-webhook-cert
  namespace: my-system
spec:
  secretName: my-operator-webhook-tls
  dnsNames:
    - my-operator-webhook-service.my-system.svc
    - my-operator-webhook-service.my-system.svc.cluster.local
  issuerRef:
    name: selfsigned-issuer
    kind: ClusterIssuer
EOF
```

## 7. Controller와 Operator 패턴

### Reconcile Loop의 핵심 개념

Controller(컨트롤러)의 역할은 단순합니다. **"현재 상태를 원하는 상태로 만들어라."**

```
사용자: kubectl apply -f cr.yaml
          (spec.replicas: 5로 선언)
               │
               ▼
    API 서버에 CR 저장
               │
               ▼ (Watch API로 이벤트 수신)
    컨트롤러: "spec.replicas=5인데 실제 파드는 3개"
               │
               ▼
    Deployment.replicas = 5 로 업데이트
               │
               ▼
    상태 확인 후 status.readyReplicas = 5 업데이트
               │
               ▼
    다시 감시... (무한 반복)
```

이 방식의 장점은 **멱등성(idempotency)**입니다. 동일한 입력으로 몇 번 실행해도 결과가 같습니다. 컨트롤러가 재시작되거나 일시적인 오류가 생겨도 결국 원하는 상태에 도달합니다.

### Reconcile 결과

`ctrl.Result`를 반환해 다음 처리를 제어합니다.

```go
// 더 이상 처리 불필요 — 변경 이벤트가 올 때 다시 처리
return ctrl.Result{}, nil

// 에러 — 지수 백오프(exponential backoff)로 자동 재처리
return ctrl.Result{}, fmt.Errorf("처리 실패: %w", err)

// 지정 시간 후 강제 재처리 (외부 상태를 주기적으로 확인할 때)
return ctrl.Result{RequeueAfter: 30 * time.Second}, nil

// 즉시 재처리 (다음 큐 처리 사이클에)
return ctrl.Result{Requeue: true}, nil
```

### 전체 Reconciler 구현

```go
package controller

import (
    "context"
    "fmt"

    appsv1 "k8s.io/api/apps/v1"
    corev1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/api/errors"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/log"

    examplev1 "github.com/myorg/my-operator/api/v1"
)

// MyAppReconciler: MyApp CR을 감시하고 조정하는 컨트롤러
type MyAppReconciler struct {
    client.Client          // Kubernetes API 클라이언트
    Scheme *runtime.Scheme // 타입 등록 정보
}

// Reconcile: 핵심 조정 로직. CR 변경 시마다 호출됨
func (r *MyAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    logger := log.FromContext(ctx)
    logger.Info("Reconcile 시작", "name", req.Name, "namespace", req.Namespace)

    // ── 1. CR 조회 ──────────────────────────────────────────────────
    myApp := &examplev1.MyApp{}
    if err := r.Get(ctx, req.NamespacedName, myApp); err != nil {
        if errors.IsNotFound(err) {
            // CR이 삭제됨 — 이미 정리됐거나 Finalizer가 처리했을 것
            logger.Info("MyApp을 찾을 수 없음, 이미 삭제됨")
            return ctrl.Result{}, nil
        }
        return ctrl.Result{}, fmt.Errorf("MyApp 조회 실패: %w", err)
    }

    // ── 2. 삭제 처리 (Finalizer 패턴) ───────────────────────────────
    finalizerName := "example.com/cleanup-finalizer"

    if !myApp.DeletionTimestamp.IsZero() {
        // 삭제 마크가 있음 — 정리 작업 수행 후 Finalizer 제거
        if containsString(myApp.Finalizers, finalizerName) {
            logger.Info("외부 리소스 정리 중...")
            if err := r.cleanupExternalResources(ctx, myApp); err != nil {
                return ctrl.Result{}, fmt.Errorf("정리 실패: %w", err)
            }
            // Finalizer 제거 — 이후 Kubernetes가 CR을 실제 삭제
            myApp.Finalizers = removeString(myApp.Finalizers, finalizerName)
            if err := r.Update(ctx, myApp); err != nil {
                return ctrl.Result{}, err
            }
        }
        return ctrl.Result{}, nil
    }

    // Finalizer 등록 (아직 없으면)
    if !containsString(myApp.Finalizers, finalizerName) {
        myApp.Finalizers = append(myApp.Finalizers, finalizerName)
        if err := r.Update(ctx, myApp); err != nil {
            return ctrl.Result{}, err
        }
        // Update 후에는 Reconcile이 다시 트리거됨
        return ctrl.Result{}, nil
    }

    // ── 3. Deployment 조정 ──────────────────────────────────────────
    if err := r.reconcileDeployment(ctx, myApp); err != nil {
        return ctrl.Result{}, fmt.Errorf("Deployment 조정 실패: %w", err)
    }

    // ── 4. Status 업데이트 ───────────────────────────────────────────
    // Status()를 통해 /status 서브리소스만 업데이트 (spec은 변경 안 됨)
    myApp.Status.Phase = "Running"
    myApp.Status.ReadyReplicas = myApp.Spec.Replicas
    if err := r.Status().Update(ctx, myApp); err != nil {
        return ctrl.Result{}, fmt.Errorf("Status 업데이트 실패: %w", err)
    }

    logger.Info("Reconcile 완료")
    // 외부 상태를 주기적으로 확인해야 한다면 RequeueAfter 사용
    return ctrl.Result{}, nil
}

// reconcileDeployment: MyApp에 맞는 Deployment를 생성 또는 업데이트
func (r *MyAppReconciler) reconcileDeployment(ctx context.Context, myApp *examplev1.MyApp) error {
    logger := log.FromContext(ctx)

    // 원하는 Deployment 상태 정의
    desired := r.buildDeployment(myApp)

    // 현재 Deployment 조회
    existing := &appsv1.Deployment{}
    err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing)

    if errors.IsNotFound(err) {
        // 없으면 생성
        logger.Info("Deployment 생성")
        return r.Create(ctx, desired)
    }
    if err != nil {
        return err
    }

    // 있으면 업데이트 (필요한 필드만)
    existing.Spec.Replicas = desired.Spec.Replicas
    existing.Spec.Template.Spec.Containers[0].Image = desired.Spec.Template.Spec.Containers[0].Image
    logger.Info("Deployment 업데이트")
    return r.Update(ctx, existing)
}

// buildDeployment: MyApp 스펙으로 Deployment 객체 생성
func (r *MyAppReconciler) buildDeployment(myApp *examplev1.MyApp) *appsv1.Deployment {
    labels := map[string]string{"app": myApp.Name}
    replicas := myApp.Spec.Replicas

    return &appsv1.Deployment{
        ObjectMeta: metav1.ObjectMeta{
            Name:      myApp.Name,
            Namespace: myApp.Namespace,
            // OwnerReference: Deployment가 MyApp에 소유됨
            // MyApp 삭제 시 Deployment도 자동 삭제 (garbage collection)
            OwnerReferences: []metav1.OwnerReference{
                *metav1.NewControllerRef(myApp, examplev1.GroupVersionKind),
            },
        },
        Spec: appsv1.DeploymentSpec{
            Replicas: &replicas,
            Selector: &metav1.LabelSelector{MatchLabels: labels},
            Template: corev1.PodTemplateSpec{
                ObjectMeta: metav1.ObjectMeta{Labels: labels},
                Spec: corev1.PodSpec{
                    Containers: []corev1.Container{
                        {
                            Name:  "app",
                            Image: myApp.Spec.Image,
                        },
                    },
                },
            },
        },
    }
}

func (r *MyAppReconciler) cleanupExternalResources(ctx context.Context, myApp *examplev1.MyApp) error {
    // 외부 API 호출, 클라우드 리소스 정리 등
    log.FromContext(ctx).Info("외부 리소스 정리 완료")
    return nil
}

// SetupWithManager: Manager에 컨트롤러 등록
func (r *MyAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&examplev1.MyApp{}).          // 주 감시 대상: MyApp CR
        Owns(&appsv1.Deployment{}).       // 소유한 Deployment 변경 시에도 재처리
        // Watches(...): 소유하지 않은 리소스 변경도 감시할 수 있음
        Complete(r)
}

// 헬퍼 함수
func containsString(slice []string, s string) bool {
    for _, item := range slice { if item == s { return true } }
    return false
}

func removeString(slice []string, s string) (result []string) {
    for _, item := range slice { if item != s { result = append(result, item) } }
    return
}
```

## 8. kubebuilder로 Operator 만들기

kubebuilder는 Kubernetes Operator를 위한 공식 스캐폴딩 도구입니다. CRD YAML, RBAC, Webhook, Dockerfile, Makefile 등 보일러플레이트를 자동으로 생성합니다.

### kubebuilder 설치

```bash
# Linux
curl -L -o kubebuilder \
  https://github.com/kubernetes-sigs/kubebuilder/releases/latest/download/kubebuilder_linux_amd64
chmod +x kubebuilder
sudo mv kubebuilder /usr/local/bin/

# macOS
brew install kubebuilder

# 버전 확인
kubebuilder version
```

### 프로젝트 생성부터 배포까지

```bash
# ── 1단계: 프로젝트 초기화 ──────────────────────────────────────────
mkdir my-operator && cd my-operator

kubebuilder init \
  --domain example.com \
  --repo github.com/myorg/my-operator

# 생성 파일:
# cmd/main.go          — 엔트리포인트 (Manager 초기화)
# config/             — 쿠버네티스 manifest 디렉토리
# go.mod, go.sum      — 의존성

# ── 2단계: API 생성 (CRD + Controller 스캐폴딩) ─────────────────────
kubebuilder create api \
  --group apps \
  --version v1 \
  --kind MyApp

# 생성 파일:
# api/v1/myapp_types.go             — Go 타입 정의 (수정 필요)
# internal/controller/myapp_controller.go  — Reconcile 로직 (수정 필요)
# config/crd/                        — CRD manifest (자동 생성)
# config/samples/apps_v1_myapp.yaml  — CR 예제

# ── 3단계: api/v1/myapp_types.go 수정 ─────────────────────────────
# MyAppSpec, MyAppStatus 구조체에 필드 추가

# ── 4단계: manifest 및 Go 코드 생성 ─────────────────────────────────
# controller-gen이 어노테이션을 읽어 CRD YAML, RBAC, deepcopy 등 생성
make manifests  # CRD YAML, RBAC 생성
make generate   # deepcopy, runtime.Object 구현 코드 생성

# ── 5단계: CRD를 클러스터에 설치 ──────────────────────────────────
make install

# 설치 확인
kubectl get crd myapps.apps.example.com

# ── 6단계: 로컬에서 컨트롤러 실행 (개발 시 빠른 반복) ──────────────
# 클러스터에 배포하지 않고 로컬 프로세스로 실행
# ~/.kube/config의 현재 컨텍스트 클러스터에 연결
make run

# ── 7단계: 테스트용 CR 생성 (다른 터미널) ──────────────────────────
kubectl apply -f config/samples/apps_v1_myapp.yaml
kubectl get myapps

# ── 8단계: 이미지 빌드 및 클러스터에 배포 ──────────────────────────
# 이미지 빌드 및 레지스트리 푸시
make docker-build docker-push IMG=myorg/my-operator:v0.1.0

# 클러스터에 Operator 배포
make deploy IMG=myorg/my-operator:v0.1.0

# 배포 확인
kubectl get deploy -n my-operator-system
kubectl logs -f deploy/my-operator-controller-manager -n my-operator-system

# ── 9단계: 정리 ────────────────────────────────────────────────────
make undeploy   # Operator 삭제
make uninstall  # CRD 삭제
```

### Go 타입 정의 (api/v1/myapp_types.go)

```go
package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// MyAppSpec: 사용자가 선언하는 원하는 상태
type MyAppSpec struct {
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=100
    Replicas int32 `json:"replicas"`

    // +kubebuilder:validation:Pattern=`^[\w.\-/]+:[\w.\-]+$`
    Image string `json:"image"`

    // +optional
    Config map[string]string `json:"config,omitempty"`

    // +kubebuilder:default:=RollingUpdate
    // +kubebuilder:validation:Enum=RollingUpdate;Recreate
    // +optional
    Strategy string `json:"strategy,omitempty"`
}

// MyAppStatus: 컨트롤러가 기록하는 현재 상태
type MyAppStatus struct {
    // +optional
    ReadyReplicas int32 `json:"readyReplicas,omitempty"`

    // +optional
    Phase string `json:"phase,omitempty"`

    // +optional
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// MyApp: CRD 루트 타입
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.readyReplicas
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type MyApp struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   MyAppSpec   `json:"spec,omitempty"`
    Status MyAppStatus `json:"status,omitempty"`
}

// MyAppList: kubectl get myapps 시 반환되는 목록 타입
// +kubebuilder:object:root=true
type MyAppList struct {
    metav1.TypeMeta `json:",inline"`
    metav1.ListMeta `json:"metadata,omitempty"`
    Items           []MyApp `json:"items"`
}

func init() {
    SchemeBuilder.Register(&MyApp{}, &MyAppList{})
}
```

### RBAC 어노테이션

컨트롤러 파일 상단에 주석으로 선언하면 `make manifests`가 자동으로 `ClusterRole`을 생성합니다.

```go
// internal/controller/myapp_controller.go

// MyApp CR 읽기/쓰기
// +kubebuilder:rbac:groups=apps.example.com,resources=myapps,verbs=get;list;watch;create;update;patch;delete
// Status 서브리소스 업데이트
// +kubebuilder:rbac:groups=apps.example.com,resources=myapps/status,verbs=get;update;patch
// Finalizer 업데이트
// +kubebuilder:rbac:groups=apps.example.com,resources=myapps/finalizers,verbs=update

// 소유할 리소스 — Deployment 생성/수정
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// Service 관리
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// 이벤트 기록 (선택)
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
```

```bash
# 어노테이션 → ClusterRole YAML 생성
make manifests

# 생성된 파일 확인
cat config/rbac/role.yaml
```

### 생성되는 프로젝트 구조

```
my-operator/
├── api/
│   └── v1/
│       ├── myapp_types.go            # ← 직접 수정: 타입 정의
│       ├── groupversion_info.go      # API 그룹/버전 등록
│       └── zz_generated.deepcopy.go  # make generate 자동 생성
├── cmd/
│   └── main.go                       # Manager 초기화 (보통 수정 불필요)
├── config/
│   ├── crd/bases/                    # make manifests 생성
│   ├── rbac/                         # RBAC 파일들
│   ├── manager/                      # Operator Deployment
│   ├── default/                      # kustomize 기반 배포 설정
│   └── samples/                      # CR 예제 (직접 수정)
├── internal/
│   └── controller/
│       ├── myapp_controller.go       # ← 직접 수정: Reconcile 로직
│       └── myapp_controller_test.go  # 통합 테스트
├── Dockerfile                        # 멀티스테이지 빌드
├── Makefile                          # 빌드/배포 명령
└── go.mod
```

> **한 줄 요약**: kubebuilder로 뼈대를 만들고 → `api/v1/*_types.go`에 타입을 정의하고 → `internal/controller/*_controller.go`에 Reconcile 로직을 작성합니다. 나머지(CRD YAML, RBAC, deepcopy 등)는 `make manifests && make generate`가 처리합니다.
