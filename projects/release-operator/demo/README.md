# CanaryRelease Demo

Operator 변경 없이 시연용 데모 앱과 대시보드를 제공합니다.

## Docker Hub 이미지

| 이미지 | 용도 |
|--------|------|
| `jangwoojung/canary-demo-app:stable` | stable Deployment |
| `jangwoojung/canary-demo-app:canary` | CanaryRelease spec.image |
| `jangwoojung/canary-demo-dashboard:v0.1.1` | 시연 대시보드 |

## 빌드 및 푸시

```bash
cd projects/release-operator
make -f demo/Makefile docker-build-app docker-build-dashboard
make -f demo/Makefile docker-push-app docker-push-dashboard
```

## 클러스터 적용

```bash
kubectl apply -k config/samples/
kubectl apply -k demo/manifests/
```

## 시연

```bash
kubectl port-forward -n team-5 svc/canary-demo-dashboard 3000:8080
# 브라우저 http://localhost:3000
```

대시보드에서 **시작 / 리셋 / 장애 주입** 버튼으로 전체 시나리오를 진행합니다.

## 리셋 (CLI)

```bash
./demo/scripts/reset.sh
```
