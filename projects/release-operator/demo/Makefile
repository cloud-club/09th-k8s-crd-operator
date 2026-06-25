REGISTRY ?= jangwoojung
TAG ?= v0.1.2

DOCKER_PLATFORM ?= linux/amd64

.PHONY: docker-build-app docker-push-app docker-build-dashboard docker-push-dashboard

# Run from projects/release-operator: make -f demo/Makefile <target>

docker-build-app:
	docker build --platform $(DOCKER_PLATFORM) --build-arg VERSION="Stable v1" -t $(REGISTRY)/canary-demo-app:stable demo/app
	docker build --platform $(DOCKER_PLATFORM) --build-arg VERSION="Canary v2" -t $(REGISTRY)/canary-demo-app:canary demo/app

docker-push-app:
	docker push $(REGISTRY)/canary-demo-app:stable
	docker push $(REGISTRY)/canary-demo-app:canary

docker-build-dashboard:
	docker build --platform $(DOCKER_PLATFORM) -f demo/dashboard/Dockerfile -t $(REGISTRY)/canary-demo-dashboard:$(TAG) .

docker-push-dashboard:
	docker push $(REGISTRY)/canary-demo-dashboard:$(TAG)
