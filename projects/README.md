# Projects 가이드

---

## 목차

1. [디렉터리 구조](#1-디렉터리-구조)
2. [사전 준비](#2-사전-준비)
3. [프로젝트 초기 생성](#3-프로젝트-초기-생성)
4. [CRD 및 Controller 추가](#4-crd-및-controller-추가)
5. [변경사항을 클러스터에 적용하기](#5-변경사항을-클러스터에-적용하기)
6. [팀별 Operator 목록](#7-팀별-operator-목록)

---

## 1. 디렉터리 구조

```
09th-k8s-crd-operator/
│
├── README.md
└── projects/                        ← 이 디렉터리
    ├── README.md                    ← 현재 문서
    ├── database-operator/           ← Team 1
    ├── kafka-operator/              ← Team 2
    ├── cronschedule-operator/       ← Team 3
    ├── observability-operator/      ← Team 4
    └── release-operator/            ← Team 5
```

각 Operator 디렉터리는 **완전히 독립된 Operator SDK 프로젝트**입니다.
자신의 `go.mod`, `main.go`, `Makefile`을 갖고, 빌드·테스트·배포가 독립적으로 이루어집니다.

---

## 2. 사전 준비

프로젝트를 생성하기 전에 아래 도구가 설치되어 있어야 합니다.

| 도구 | 설치 확인 | 권장 버전 |
|------|-----------|-----------|
| Go | `go version` | 1.21 이상 |
| operator-sdk | `operator-sdk version` | v1.34 이상 |
| kubectl | `kubectl version --client` | v1.28 이상 |
| make | `make --version` | - |


---

## 3. 프로젝트 초기 생성

> **주의:** 프로젝트는 반드시 `projects/<operator-name>/` 안에서 초기화합니다.
> 다른 팀의 디렉터리나 Repository 루트에서 초기화하지 않습니다.

### 3-1. Repository 최신화 및 브랜치 생성

```bash
git checkout main
git pull origin main
git checkout -b feature/<operator-name>/init
```

예시 (Team 1 - Database Operator):

```bash
git checkout -b feature/database/init
```

### 3-2. 프로젝트 디렉터리 생성 및 이동

```bash
mkdir -p projects/<operator-name>
cd projects/<operator-name>
```

예시:

```bash
mkdir -p projects/database-operator
cd projects/database-operator
```

### 3-3. operator-sdk init

```bash
operator-sdk init \
  --domain <your-domain> \
  --repo github.com/<org>/<repo>/projects/<operator-name>
```

실행 후 생성되는 파일 구조:

```
database-operator/
├── cmd/
│   └── main.go
├── config/
│   ├── default/
│   ├── manager/
│   ├── prometheus/
│   └── rbac/
├── Dockerfile
├── go.mod
├── go.sum
├── Makefile
└── PROJECT
```

### 3-4. 생성 확인

```bash
cat PROJECT       # 프로젝트 메타데이터 확인
go mod tidy       # 의존성 정리
make build        # 빌드 확인
```

---

## 4. CRD 및 Controller 추가

### 4-1. API (CRD) 생성

```bash
operator-sdk create api \
  --group <group> \
  --version <version> \
  --kind <Kind> \
  --resource \
  --controller
```

> `<version>`은 팀에서 정한 API 버전을 사용합니다. (예: `v1alpha1`, `v1beta1`, `v1`)

예시 (Database Operator):

```bash
operator-sdk create api \
  --group db \
  --version v1alpha1 \
  --kind Database \
  --resource \
  --controller
```

실행 후 추가되는 파일:

```
database-operator/
├── api/
│   └── v1alpha1/
│       ├── database_types.go      ← CRD Spec/Status 정의
│       └── groupversion_info.go
├── internal/
│   └── controller/
│       └── database_controller.go ← Reconcile 로직
└── config/
    └── crd/
        └── bases/                 ← 자동 생성되는 CRD YAML
```

### 4-2. Spec 정의 예시

`api/v1alpha1/database_types.go`에서 Spec과 Status를 정의합니다.

```go
type DatabaseSpec struct {
    Engine  string `json:"engine"`
    Version string `json:"version"`
    Replica int32  `json:"replica,omitempty"`
}

type DatabaseStatus struct {
    Phase   string `json:"phase,omitempty"`
    Message string `json:"message,omitempty"`
}
```

### 4-3. Manifest 재생성

Spec을 수정할 때마다 아래 명령으로 CRD YAML을 다시 생성합니다.

```bash
make manifests   # config/crd/bases/*.yaml 재생성
make generate    # zz_generated.deepcopy.go 재생성
```

---

## 5. 변경사항을 클러스터에 적용하기

변경사항을 클러스터에 반영하는 방법은 **개발 단계**에 따라 다릅니다.

```
[클러스터 준비]  →  [CRD 설치]  →  [Controller 로컬 실행]  →  [CR 적용 및 테스트]
```

### 5-1. 사전 조건: 클러스터 접근 확인

```bash
kubectl cluster-info
kubectl get nodes
```

개발 환경은 개인 로컬 환경의 클러스터를 활용하고, 실제 환경에서는 가비아 클라우드로 클러스터 전환 후 배포합니다.

---

### 5-2. CRD를 클러스터에 설치

코드를 수정한 뒤 CRD를 클러스터에 적용합니다.

```bash
cd projects/<operator-name>

# 1. CRD YAML 재생성
make manifests

# 2. 클러스터에 CRD 설치
make install
```

설치 확인:

```bash
kubectl get crds
# 예: databases.db.gachon.ac.kr
```

CRD를 제거하려면:

```bash
make uninstall
```

---

### 5-3. Controller를 로컬에서 실행 (개발 단계)

이미지를 빌드하지 않고 로컬 프로세스로 Controller를 실행합니다.
코드를 빠르게 수정하고 테스트할 때 사용합니다.

```bash
make run
```

> `make run`은 현재 kubeconfig의 클러스터에 연결하여 Reconcile 루프를 실행합니다.
> 터미널을 유지한 상태에서 CR을 apply하면 바로 Reconcile이 동작하는 것을 확인할 수 있습니다.

CR(Custom Resource) 예시 적용:

```bash
kubectl apply -f config/samples/db_v1alpha1_database.yaml
kubectl get databases
kubectl describe database database-sample
```

종료하려면 `Ctrl+C`를 누릅니다.

---

### 5-4. 전체 흐름 요약

```bash
# 최초 1회: 프로젝트 초기화 후
make manifests && make install

# 개발 중 반복 (코드 수정 후)
make manifests    # Spec을 바꿨을 때만
make run          # 로컬에서 Controller 실행

# 별도 터미널에서 CR 적용 및 확인
kubectl apply -f config/samples/...
kubectl get <resource>
kubectl describe <resource> <name>

# 종료: make run 터미널에서 Ctrl+C
# CRD 제거가 필요한 경우
make uninstall
```

---

### git add 시 주의

```bash
# 올바른 방법: 자신의 디렉터리만 staging
git add projects/database-operator/

# 하지 말 것: 전체 staging
git add .
```

---

## 6. 팀별 Operator 목록

| 팀 | Operator | 디렉터리 | 브랜치 prefix |
|----|----------|----------|----------------|
| Team 1 | Database Operator | `projects/database-operator/` | `feature/database/` |
| Team 2 | Kafka Topic Operator | `projects/kafka-operator/` | `feature/kafka/` |
| Team 3 | CronSchedule Operator | `projects/cronschedule-operator/` | `feature/cronschedule/` |
| Team 4 | Observability Operator | `projects/observability-operator/` | `feature/observability/` |
| Team 5 | Release Operator | `projects/release-operator/` | `feature/release/` |