# Makefile für tsmetrics
#
# Development Workflow:
# 1. cp .env.example .env
# 2. Edit .env with your Tailscale OAuth credentials
# 3. make dev  # Starts development server with live reload
#
# All environment variables are managed in scripts/env-config.sh
# Build metadata is auto-generated from git and current time

APP_NAME := tsmetrics
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
DOCKER_IMAGE := ghcr.io/sbaerlocher/$(APP_NAME)

# Go Build Targets
.PHONY: build
build:
	@./scripts/build.sh

.PHONY: test
test:
	go test -v ./...

.PHONY: run
run:
	go run .

.PHONY: clean
clean:
	rm -rf bin/

.PHONY: clean-port
clean-port:
	@echo "🧹 Cleaning up port 9100..."
	@lsof -ti:9100 | xargs -r kill -9 || echo "No process found on port 9100"
	@echo "✅ Port 9100 is now free"

.PHONY: clean-all
clean-all: clean clean-port
	@echo "🧹 Full cleanup completed (build artifacts + port 9100)"

# Docker Targets
.PHONY: docker-build
docker-build:
	docker build -t $(DOCKER_IMAGE):$(VERSION) .
	docker tag $(DOCKER_IMAGE):$(VERSION) $(DOCKER_IMAGE):latest

.PHONY: docker-push
docker-push:
	docker push $(DOCKER_IMAGE):$(VERSION)
	docker push $(DOCKER_IMAGE):latest

.PHONY: docker-run
docker-run:
	docker run --rm -it \
		-e OAUTH_CLIENT_ID=${OAUTH_CLIENT_ID} \
		-e OAUTH_CLIENT_SECRET=${OAUTH_CLIENT_SECRET} \
		-e TAILNET_NAME=${TAILNET_NAME} \
		-p 9100:9100 \
		$(DOCKER_IMAGE):latest

.PHONY: dev-tsnet
dev-tsnet:
	@./scripts/dev.sh

.PHONY: run-tsnet
run-tsnet: build
	@echo "Running tsmetrics with tsnet locally"
	@./scripts/env-config.sh && ./bin/$(APP_NAME)

.PHONY: docker-run-tsnet
docker-run-tsnet:
	docker run --rm -it \
		-e USE_TSNET=true \
		-e TSNET_HOSTNAME=tsmetrics \
		-e TSNET_STATE_DIR=/tmp/tsnet-state \
		-e TSNET_TAGS=${TSNET_TAGS:-exporter} \
		-e TS_AUTHKEY=${TS_AUTHKEY} \
		-e REQUIRE_EXPORTER_TAG=${REQUIRE_EXPORTER_TAG:-true} \
		-e TARGET_DEVICES=${TARGET_DEVICES:-gateway-140207,gateway-130104} \
		-e ENV=${ENV:-production} \
		-e LOG_LEVEL=${LOG_LEVEL:-info} \
		-e LOG_FORMAT=${LOG_FORMAT:-text} \
		-e CLIENT_METRICS_TIMEOUT=${CLIENT_METRICS_TIMEOUT:-10s} \
		-e MAX_CONCURRENT_SCRAPES=${MAX_CONCURRENT_SCRAPES:-10} \
		-e CLIENT_METRICS_PORT=${CLIENT_METRICS_PORT:-5252} \
		-e OAUTH_CLIENT_ID=${OAUTH_CLIENT_ID} \
		-e OAUTH_CLIENT_SECRET=${OAUTH_CLIENT_SECRET} \
		-e TAILNET_NAME=${TAILNET_NAME} \
		-v tsnet-state:/tmp/tsnet-state \
		$(DOCKER_IMAGE):latest

# Kubernetes Targets (Plain YAML)
.PHONY: k8s-deploy
k8s-deploy:
	kubectl apply -f deploy/kubernetes.yaml

.PHONY: k8s-delete
k8s-delete:
	kubectl delete -f deploy/kubernetes.yaml

# Helm Targets
.PHONY: helm-install
helm-install:
	helm install tsmetrics deploy/helm/tsmetrics

.PHONY: helm-upgrade
helm-upgrade:
	helm upgrade tsmetrics deploy/helm/tsmetrics

.PHONY: helm-uninstall
helm-uninstall:
	helm uninstall tsmetrics

.PHONY: helm-template
helm-template:
	helm template tsmetrics deploy/helm/tsmetrics

.PHONY: helm-lint
helm-lint:
	helm lint deploy/helm/tsmetrics

# Kustomize Targets
.PHONY: kustomize-dev
kustomize-dev:
	kubectl apply -k deploy/kustomize/overlays/development

.PHONY: kustomize-prod
kustomize-prod:
	kubectl apply -k deploy/kustomize/overlays/production

.PHONY: kustomize-delete-dev
kustomize-delete-dev:
	kubectl delete -k deploy/kustomize/overlays/development

.PHONY: kustomize-delete-prod
kustomize-delete-prod:
	kubectl delete -k deploy/kustomize/overlays/production

.PHONY: kustomize-preview-dev
kustomize-preview-dev:
	kubectl kustomize deploy/kustomize/overlays/development

.PHONY: kustomize-preview-prod
kustomize-preview-prod:
	kubectl kustomize deploy/kustomize/overlays/production

# Systemd Targets
.PHONY: systemd-install
systemd-install: build
	sudo deploy/install-systemd.sh install bin/$(APP_NAME)

.PHONY: systemd-uninstall
systemd-uninstall:
	sudo deploy/install-systemd.sh uninstall

.PHONY: systemd-start
systemd-start:
	sudo deploy/install-systemd.sh start

.PHONY: systemd-stop
systemd-stop:
	sudo deploy/install-systemd.sh stop

.PHONY: systemd-status
systemd-status:
	deploy/install-systemd.sh status

# Development Targets
.PHONY: dev-deps
dev-deps:
	go mod tidy
	go mod download

.PHONY: lint
lint:
	golangci-lint run

.PHONY: fmt
fmt:
	go fmt ./...

# CI/CD Targets
.PHONY: ci-test
ci-test:
	@echo "🧪 Running CI tests locally..."
	go vet ./...
	go fmt ./...
	go test -v -race -coverprofile=coverage.out ./...
	@if [ -d "tests/integration" ]; then \
		echo "Running integration tests..."; \
		go test -v -tags=integration ./tests/integration/...; \
	fi

.PHONY: ci-security
ci-security:
	@echo "🔒 Running security scans..."
	@command -v gosec >/dev/null 2>&1 || { echo "Installing gosec..."; go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest; }
	gosec ./...

.PHONY: ci-docker
ci-docker:
	@echo "🐳 Testing Docker build..."
	docker build -t $(DOCKER_IMAGE):test .
	docker run --rm $(DOCKER_IMAGE):test --help || true

.PHONY: ci-helm
ci-helm:
	@echo "⚓ Testing Helm chart..."
	helm lint deploy/helm/tsmetrics
	helm template test-release deploy/helm/tsmetrics \
		--set tailscale.oauthClientId=test \
		--set tailscale.oauthClientSecret=test \
		--set tailscale.tailnetName=test.ts.net > /dev/null
	@echo "✅ Helm chart validation passed"

.PHONY: ci-all
ci-all: ci-test ci-security ci-docker ci-helm
	@echo "🎉 All CI checks passed!"

.PHONY: workflow-test
workflow-test: ci-all
	@echo "🚀 Local workflow simulation completed"

# All-in-one targets
.PHONY: dev
dev:
	@./scripts/dev.sh

.PHONY: dev-direct
dev-direct:
	@echo "🚀 Starting development environment with go run"
	@source scripts/env-config.sh && scripts/env-config.sh && go run .

.PHONY: release
release: clean test build docker-build docker-push

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build          - Build the binary"
	@echo "  test           - Run tests"
	@echo "  run            - Run locally without environment setup"
	@echo "  clean          - Remove build artifacts"
	@echo "  clean-port     - Kill processes on port 9100"
	@echo "  clean-all      - Full cleanup (build + port)"
	@echo ""
	@echo "Development targets:"
	@echo "  dev            - Start development with air live reload (via scripts/dev.sh)"
	@echo "  dev-direct     - Start development with go run (uses env config)"
	@echo "  dev-tsnet      - Alias for dev with tsnet enabled"
	@echo "  run-tsnet      - Build and run with tsnet configuration"
	@echo ""
	@echo "Docker targets:"
	@echo "  docker-build   - Build Docker image"
	@echo "  docker-run     - Run Docker container"
	@echo "  docker-run-tsnet - Run Docker container with tsnet"
	@echo "  docker-push    - Push Docker image to registry"
	@echo ""
	@echo "Kubernetes targets:"
	@echo "  k8s-deploy     - Deploy with plain YAML"
	@echo "  k8s-delete     - Remove plain YAML deployment"
	@echo ""
	@echo "Helm targets:"
	@echo "  helm-install   - Install with Helm"
	@echo "  helm-upgrade   - Upgrade with Helm"
	@echo "  helm-uninstall - Uninstall with Helm"
	@echo "  helm-template  - Preview Helm templates"
	@echo "  helm-lint      - Lint Helm chart"
	@echo ""
	@echo "Kustomize targets:"
	@echo "  kustomize-dev        - Deploy development with Kustomize"
	@echo "  kustomize-prod       - Deploy production with Kustomize"
	@echo "  kustomize-delete-dev - Remove development deployment"
	@echo "  kustomize-delete-prod - Remove production deployment"
	@echo "  kustomize-preview-dev - Preview development Kustomize"
	@echo "  kustomize-preview-prod - Preview production Kustomize"
	@echo ""
	@echo "Systemd targets:"
	@echo "  systemd-install  - Install as systemd service"
	@echo "  systemd-uninstall - Remove systemd service"
	@echo "  systemd-start    - Start systemd service"
	@echo "  systemd-stop     - Stop systemd service"
	@echo "  systemd-status   - Show systemd service status"
	@echo ""
	@echo "Quality targets:"
	@echo "  lint           - Run golangci-lint"
	@echo "  fmt            - Format Go code"
	@echo "  dev-deps       - Install development dependencies"
	@echo ""
	@echo "CI/CD targets:"
	@echo "  ci-test        - Run CI tests locally"
	@echo "  ci-security    - Run security scans"
	@echo "  ci-docker      - Test Docker build"
	@echo "  ci-helm        - Test Helm chart"
	@echo "  ci-all         - Run all CI checks"
	@echo "  workflow-test  - Simulate full workflow"
	@echo ""
	@echo "  release        - Full release pipeline (clean, test, build, docker)"
