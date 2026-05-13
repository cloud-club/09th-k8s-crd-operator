# 개발 환경 구성 — Go · Docker · Kubernetes

> Go 기반 Kubernetes Operator 개발에 필요한 도구들을 처음부터 설치하고 설정하는 과정을 정리합니다.
> 로컬 머신에서 실제 Kubernetes 클러스터처럼 개발·테스트할 수 있는 환경을 목표로 합니다.

## 목차

1. [전체 구성 개요](#1-전체-구성-개요)
2. [Go 설치 및 설정](#2-go-설치-및-설정)
3. [Docker 설치 및 기본 개념](#3-docker-설치-및-기본-개념)
4. [로컬 Kubernetes 클러스터](#4-로컬-kubernetes-클러스터)
5. [kubectl 설치 및 설정](#5-kubectl-설치-및-설정)
6. [도구 연결 흐름 정리](#6-도구-연결-흐름-정리)

## 1. 전체 구성 개요

Kubernetes Operator를 개발하려면 다음 네 가지 도구가 유기적으로 연결됩니다.

```
[Go 소스 코드]
      │  go build
      ▼
[바이너리 / Docker 이미지]
      │  docker build & push
      ▼
[컨테이너 레지스트리]
      │  kubectl apply / helm install
      ▼
[Kubernetes 클러스터] ◄─── kubectl (조회·디버깅)
```

| 도구                | 역할                                                                          |
| ------------------- | ----------------------------------------------------------------------------- |
| **Go**              | Operator 로직을 작성하는 언어. controller-runtime, client-go 생태계가 Go 중심 |
| **Docker**          | Operator를 컨테이너 이미지로 패키징. 클러스터에서 실행되는 단위               |
| **minikube / kind** | 로컬 PC에서 실제 Kubernetes처럼 동작하는 클러스터 제공                        |
| **kubectl**         | 클러스터에 리소스를 배포·조회·디버깅하는 CLI                                  |

## 2. Go 설치 및 설정

### 왜 Go인가?

Kubernetes 자체가 Go로 작성되어 있고, 공식 SDK(`client-go`, `controller-runtime`, `kubebuilder`)가 모두 Go 우선으로 관리됩니다. 덕분에 API 타입을 Go 구조체로 직접 다루고, 코드 생성 도구(`controller-gen`)도 Go 어노테이션 기반으로 동작합니다.

### 설치 (Linux)

Go는 단일 바이너리 아카이브를 압축 해제하는 방식으로 설치합니다. 패키지 매니저보다 버전 관리가 명확해서 공식 방식을 권장합니다.

```bash
# 1. 원하는 버전 다운로드 (https://go.dev/dl/ 에서 최신 확인)
wget https://go.dev/dl/go1.22.3.linux-amd64.tar.gz

# 2. 기존 설치가 있으면 먼저 제거
sudo rm -rf /usr/local/go

# 3. /usr/local 아래에 압축 해제
sudo tar -C /usr/local -xzf go1.22.3.linux-amd64.tar.gz

# 4. 설치 확인
/usr/local/go/bin/go version
```

### 설치 (macOS)

```bash
# Homebrew 이용 (버전 고정이 필요하면 go@1.22 같은 식으로 지정)
brew install go

go version
```

### 환경 변수 설정

Go를 어디서든 쓰려면 PATH에 추가해야 합니다. `~/.bashrc` 또는 `~/.zshrc`에 아래 내용을 추가합니다.

```bash
# Go 바이너리 경로
export PATH=$PATH:/usr/local/go/bin

# GOPATH: go install로 설치한 도구들이 여기 저장됨
export GOPATH=$HOME/go

# GOPATH/bin도 PATH에 추가 (kubebuilder, controller-gen 등을 쓰기 위해)
export PATH=$PATH:$GOPATH/bin
```

```bash
# 설정 반영
source ~/.bashrc   # 또는 source ~/.zshrc

# 환경 변수 전체 확인
go env

# 자주 확인하는 항목만
go env GOPATH GOROOT GOPROXY
```

주요 Go 환경 변수 의미:

| 변수           | 설명                                                            |
| -------------- | --------------------------------------------------------------- |
| `GOROOT`       | Go 자체가 설치된 경로 (`/usr/local/go`)                         |
| `GOPATH`       | 외부 패키지·빌드 캐시·설치된 바이너리 저장 경로                 |
| `GOPROXY`      | 모듈 다운로드 프록시. 기본값 `https://proxy.golang.org`         |
| `GONOSUMCHECK` | 체크섬 검증을 건너뛸 모듈 패턴 (사내 private 모듈 등)           |
| `CGO_ENABLED`  | C 라이브러리 연동 여부. Kubernetes 이미지에선 `0`으로 정적 빌드 |

### Go 모듈 (go.mod) 이해

Go 1.11부터 도입된 모듈 시스템으로, 프로젝트마다 의존성을 독립적으로 관리합니다. `go.mod`가 Node.js의 `package.json`, Python의 `requirements.txt` 역할을 합니다.

```bash
# 새 프로젝트 시작 — 모듈 이름은 보통 GitHub 경로로 지정
mkdir my-operator && cd my-operator
go mod init github.com/myorg/my-operator

# 외부 패키지 추가 (go.mod, go.sum 자동 업데이트)
go get k8s.io/client-go@v0.29.0
go get sigs.k8s.io/controller-runtime@v0.17.0

# 사용하지 않는 의존성 제거 + 누락된 것 추가
go mod tidy

# 의존성 벤더링 (오프라인 빌드, CI 캐싱에 유용)
go mod vendor
```

생성된 `go.mod` 예시:

```
module github.com/myorg/my-operator

go 1.22

require (
    k8s.io/api v0.29.0
    k8s.io/client-go v0.29.0
    sigs.k8s.io/controller-runtime v0.17.0
)
```

### 유용한 Go 개발 도구

```bash
# 테스트 실행
go test ./...                        # 전체 패키지
go test -v -run TestReconcile ./...  # 특정 테스트만
go test -cover ./...                 # 커버리지 포함

# 빌드
go build -o bin/manager ./cmd/main.go

# 정적 분석 (기본 포함)
go vet ./...

# 코드 포맷 (저장 시 자동 실행 권장)
gofmt -w .
goimports -w .   # import 정렬까지 처리 (go install golang.org/x/tools/cmd/goimports@latest)

# 통합 린터 (CI에 많이 쓰임)
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
golangci-lint run ./...
```

## 3. Docker 설치 및 기본 개념

### Docker가 필요한 이유

Kubernetes는 컨테이너를 실행하는 플랫폼입니다. Go로 작성한 Operator를 클러스터에 배포하려면 반드시 Docker 이미지로 패키징해야 합니다. 또한 로컬 Kubernetes(minikube/kind)도 내부적으로 컨테이너를 사용하기 때문에 Docker(또는 containerd)가 필요합니다.

### 설치 (Ubuntu)

```bash
# 기존 구버전 제거
sudo apt-get remove docker docker-engine docker.io containerd runc

# 필수 패키지 설치
sudo apt-get update
sudo apt-get install -y ca-certificates curl gnupg lsb-release

# Docker 공식 GPG 키 추가
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | \
  sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg

# 공식 저장소 등록
echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
  https://download.docker.com/linux/ubuntu \
  $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
  sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

# Docker 설치
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

# sudo 없이 실행하도록 현재 사용자를 docker 그룹에 추가
# (변경 사항은 재로그인 후 적용)
sudo usermod -aG docker $USER

# 설치 확인
docker --version
docker run hello-world
```

### 설치 (macOS)

macOS에서는 Docker Desktop을 설치하는 것이 가장 간단합니다. Docker Desktop은 GUI와 함께 `docker` CLI, `docker-compose`를 모두 제공합니다.

```bash
# Homebrew로 설치
brew install --cask docker

# Docker Desktop 실행 후 CLI 확인
docker --version
```

### Dockerfile 작성 — Go 멀티스테이지 빌드

Operator 이미지를 만들 때 **멀티스테이지 빌드**를 쓰면 최종 이미지 크기를 크게 줄일 수 있습니다. Go 컴파일러는 빌드 시에만 필요하고, 실행 시엔 컴파일된 바이너리만 있으면 됩니다.

```dockerfile
# ── Stage 1: 빌드 스테이지 ────────────────────────────────────────────
# golang:alpine은 빌드 도구가 포함된 공식 이미지
FROM golang:1.22-alpine AS builder

WORKDIR /workspace

# go.mod, go.sum 먼저 복사하여 의존성 레이어를 캐싱
# 소스 코드가 바뀌어도 의존성이 같으면 이 레이어는 재사용됨 → 빌드 속도 향상
COPY go.mod go.sum ./
RUN go mod download

# 소스 코드 복사 및 빌드
COPY . .

# CGO_ENABLED=0: C 라이브러리 의존 없이 순수 정적 바이너리 생성
# GOOS=linux: 컨테이너 환경(Linux)용 크로스 컴파일
# -a: 모든 패키지를 강제 재빌드
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -a -ldflags="-w -s" -o manager ./cmd/main.go
#                   └─ -w: 디버그 정보 제거, -s: 심볼 테이블 제거 → 이미지 크기 축소

# ── Stage 2: 실행 스테이지 ───────────────────────────────────────────
# distroless: 쉘도, 패키지 매니저도 없는 최소 이미지 → 공격 표면 최소화
FROM gcr.io/distroless/static:nonroot

WORKDIR /

# 빌드 스테이지의 바이너리만 복사 (Go 컴파일러, 소스 코드 등은 포함 안 됨)
COPY --from=builder /workspace/manager .

# nonroot 사용자(UID 65532)로 실행 — 보안 모범 사례
USER 65532:65532

ENTRYPOINT ["/manager"]
```

### 자주 쓰는 Docker 명령어

```bash
# 이미지 빌드 (-t: 이름:태그 지정, .: 현재 디렉토리의 Dockerfile 사용)
docker build -t myorg/my-operator:v0.1.0 .

# 빌드 과정을 자세히 보고 싶을 때
docker build --progress=plain -t myorg/my-operator:v0.1.0 .

# 이미지 목록 확인
docker images

# 이미지에 추가 태그 달기 (latest 등)
docker tag myorg/my-operator:v0.1.0 myorg/my-operator:latest

# Docker Hub 또는 사설 레지스트리에 푸시
docker login
docker push myorg/my-operator:v0.1.0

# 컨테이너 실행 (--rm: 종료 후 자동 삭제, -it: 대화형 터미널)
docker run --rm -it myorg/my-operator:v0.1.0

# 실행 중인 컨테이너 목록
docker ps

# 모든 컨테이너 (종료된 것 포함)
docker ps -a

# 컨테이너 로그 실시간 확인
docker logs -f <container-id>

# 실행 중인 컨테이너에 접속
docker exec -it <container-id> /bin/sh

# 이미지 레이어 구조 확인 (크기 최적화에 유용)
docker history myorg/my-operator:v0.1.0

# 불필요한 이미지·컨테이너·볼륨 일괄 삭제
docker system prune -af
```

## 4. 로컬 Kubernetes 클러스터

로컬에서 Kubernetes를 띄우는 방법은 여러 가지가 있습니다. 가장 많이 쓰는 두 가지를 소개합니다.

| 도구         | 특징                                                  | 추천 상황                   |
| ------------ | ----------------------------------------------------- | --------------------------- |
| **minikube** | VM 또는 Docker 기반. 대시보드, addon 지원. 설정 쉬움  | 입문자, 간단한 로컬 개발    |
| **kind**     | Docker 컨테이너를 노드로 사용. 가볍고 빠름. CI 친화적 | 멀티노드 테스트, CI/CD 환경 |

### minikube

```bash
# 설치 (Linux)
curl -LO https://storage.googleapis.com/minikube/releases/latest/minikube-linux-amd64
sudo install minikube-linux-amd64 /usr/local/bin/minikube

# 설치 (macOS)
brew install minikube

# 클러스터 시작
# --driver=docker: Docker를 VM 대신 사용 (가볍고 빠름)
# --cpus, --memory: 리소스 할당량 (Operator 개발엔 넉넉하게)
minikube start --driver=docker --cpus=4 --memory=8192

# 상태 확인
minikube status

# 웹 대시보드 열기 (클러스터 상태를 GUI로 확인)
minikube dashboard

# 로컬에서 빌드한 이미지를 minikube 내부로 로드
# (인터넷 레지스트리 없이 로컬 이미지 사용 가능)
minikube image load myorg/my-operator:v0.1.0

# 클러스터 일시 중지 / 재개
minikube stop
minikube start

# 클러스터 완전 삭제
minikube delete
```

> **Tip**: minikube를 사용할 때 `eval $(minikube docker-env)`를 실행하면 로컬 docker 명령이 minikube 내부 Docker 데몬을 가리키게 됩니다. 이 상태에서 `docker build`하면 이미지 로드 없이 바로 사용 가능합니다.

### kind (Kubernetes in Docker)

kind는 Docker 컨테이너 자체를 Kubernetes 노드로 사용합니다. 여러 노드를 시뮬레이션하거나 CI에서 빠르게 클러스터를 생성·삭제할 때 특히 유용합니다.

```bash
# 설치 (Go가 설치된 환경)
go install sigs.k8s.io/kind@latest

# 설치 (macOS)
brew install kind

# 단일 노드 클러스터 생성
kind create cluster --name dev

# 멀티노드 클러스터 (컨트롤플레인 1 + 워커 2)
cat <<EOF > kind-config.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
  - role: worker
  - role: worker
EOF

kind create cluster --name dev --config kind-config.yaml

# 생성된 클러스터 목록
kind get clusters

# 로컬 이미지를 kind 클러스터에 로드
# (kind는 클러스터마다 자체 이미지 저장소를 가짐)
kind load docker-image myorg/my-operator:v0.1.0 --name dev

# 클러스터 삭제
kind delete cluster --name dev
```

## 5. kubectl 설치 및 설정

kubectl은 Kubernetes API 서버와 통신하는 공식 CLI입니다. 클러스터에 리소스를 배포하고, 상태를 조회하고, 로그를 확인하는 등 모든 작업을 수행합니다.

### 설치

```bash
# 최신 stable 버전 다운로드 (Linux)
curl -LO "https://dl.k8s.io/release/$(curl -sL https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"

# 실행 권한 부여 및 시스템 경로에 설치
chmod +x kubectl
sudo mv kubectl /usr/local/bin/kubectl

# 설치 확인
kubectl version --client

# macOS
brew install kubectl
```

### kubeconfig — 클러스터 연결 설정

kubectl은 `~/.kube/config` 파일을 읽어 어느 클러스터에 어떻게 접속할지 판단합니다. minikube나 kind로 클러스터를 만들면 이 파일이 자동으로 업데이트됩니다.

```bash
# 현재 설정된 컨텍스트(클러스터 연결 정보) 목록
kubectl config get-contexts

# 예시 출력:
# CURRENT   NAME        CLUSTER     AUTHINFO    NAMESPACE
# *         kind-dev    kind-dev    kind-dev    default
#           minikube    minikube    minikube    default

# 컨텍스트 전환 (다른 클러스터로 이동)
kubectl config use-context minikube

# 여러 kubeconfig 파일 병합 (팀 클러스터 + 로컬 클러스터 함께 사용)
export KUBECONFIG=~/.kube/config:~/.kube/staging-config
kubectl config view --merge --flatten > ~/.kube/merged-config
export KUBECONFIG=~/.kube/merged-config

# 기본 네임스페이스 변경 (매번 -n을 안 써도 되게)
kubectl config set-context --current --namespace=my-namespace
```

### 자주 쓰는 kubectl 명령어

```bash
# ── 기본 조회 ────────────────────────────────────────────────────────
kubectl get nodes -o wide                     # 노드 상태 (IP, OS 등 포함)
kubectl get pods -A                           # 전체 네임스페이스 파드 조회
kubectl get pods -n kube-system               # 특정 네임스페이스
kubectl get all -n default                    # 네임스페이스 내 모든 리소스

# ── 상세 정보 ────────────────────────────────────────────────────────
kubectl describe pod <pod-name> -n <ns>       # 이벤트·상태 포함 상세 정보
kubectl describe node <node-name>

# ── 로그 ─────────────────────────────────────────────────────────────
kubectl logs <pod-name> -n <ns>               # 로그 출력
kubectl logs <pod-name> -n <ns> -f            # 실시간 스트리밍
kubectl logs <pod-name> -n <ns> --previous    # 이전 컨테이너 로그 (재시작 후)
kubectl logs <pod-name> -n <ns> -c <container-name>  # 멀티컨테이너 파드

# ── 디버깅 ───────────────────────────────────────────────────────────
kubectl exec -it <pod-name> -- /bin/sh        # 컨테이너 접속
kubectl port-forward pod/<pod-name> 8080:8080 # 로컬 포트 포워딩
kubectl port-forward svc/<svc-name> 8080:80

# ── 이벤트 (문제 원인 파악에 핵심) ──────────────────────────────────
kubectl get events -n default --sort-by='.lastTimestamp'

# ── 리소스 생성·수정·삭제 ───────────────────────────────────────────
kubectl apply -f manifest.yaml                # 선언적 적용 (없으면 생성, 있으면 갱신)
kubectl delete -f manifest.yaml               # 파일 기반 삭제
kubectl delete pod <pod-name> -n <ns>

# ── 드라이런 (실제 적용 전 미리보기) ────────────────────────────────
kubectl apply -f manifest.yaml --dry-run=client -o yaml   # 로컬 검증
kubectl apply -f manifest.yaml --dry-run=server -o yaml   # 서버 검증 (Webhook 포함)

# ── 차이점 확인 ──────────────────────────────────────────────────────
kubectl diff -f manifest.yaml                 # 현재 상태 vs 파일 비교

# ── 리소스 사용량 ────────────────────────────────────────────────────
kubectl top pods -n default
kubectl top nodes
```

## 6. 도구 연결 흐름 정리

실제 개발 사이클은 다음 순서로 반복됩니다.

```
1. Go 소스 수정
      │
      ▼
2. 로컬 테스트 (go test ./...)
      │
      ▼
3. Docker 이미지 빌드
   docker build -t myorg/my-operator:dev .
      │
      ▼
4. kind / minikube에 이미지 로드
   kind load docker-image myorg/my-operator:dev --name dev
      │
      ▼
5. CRD 및 Operator 배포
   kubectl apply -f config/crd/
   kubectl apply -f config/manager/
      │
      ▼
6. 동작 확인
   kubectl logs -f deploy/my-operator-controller-manager -n my-system
   kubectl get myapps -A
      │
      ▼
7. 문제 발견 → 1번으로 돌아가기
```

> **개발 팁**: `make run` (kubebuilder 프로젝트)을 사용하면 3~5번 과정 없이 컨트롤러를 로컬 프로세스로 직접 실행할 수 있습니다. 빠른 반복 개발 시 매우 편리합니다. 단, CRD는 미리 클러스터에 설치되어 있어야 합니다.
