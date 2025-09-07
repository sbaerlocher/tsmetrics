# Makefile für tsmetrics
#
# Development Workflow:
# 1. cp .env.example .env (optional)
# 2. Edit .env with your credentials
# 3. make dev  # Starts development server with live reload
#
# All environment variables are managed in scripts/setup-env.sh

APP_NAME := tsmetrics
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
TSNET_VERSION := $(shell go list -m -f '{{.Version}}' tailscale.com 2>/dev/null | sed 's/^v//')
VERSION_CLEAN := $(shell echo $(VERSION) | sed 's/^v//')
VERSION_LONG := $(TSNET_VERSION)-$(VERSION_CLEAN)
VERSION_SHORT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DOCKER_IMAGE := ghcr.io/sbaerlocher/$(APP_NAME)

# ==============================================================================
# CORE TARGETS
# ==============================================================================

.PHONY: build
build:
	@./scripts/build-app.sh

.PHONY: test
test:
	go test -v ./...

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
	@echo "🧹 Full cleanup completed"

# ==============================================================================
# DEVELOPMENT
# ==============================================================================

.PHONY: dev
dev:
	@./scripts/start-dev.sh

.PHONY: run
run:
	@source scripts/env-config.sh && go run ./cmd/tsmetrics

# ==============================================================================
# DOCKER
# ==============================================================================

.PHONY: docker-build
docker-build:
	docker build -t $(DOCKER_IMAGE):$(VERSION) \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		--build-arg VERSION_LONG=$(VERSION_LONG) \
		--build-arg VERSION_SHORT=$(VERSION_SHORT) .
	docker tag $(DOCKER_IMAGE):$(VERSION) $(DOCKER_IMAGE):latest

.PHONY: docker-run
docker-run:
	@echo "🐳 Starting Docker container..."
	@echo "Configure via .env file or environment variables"
	docker run --rm -it \
		--env-file .env \
		-p 9100:9100 \
		$(DOCKER_IMAGE):latest

# ==============================================================================
# HELM
# ==============================================================================

.PHONY: helm-install helm-upgrade helm-uninstall helm-template helm-lint
helm-install:
	helm install tsmetrics deploy/helm/tsmetrics

helm-upgrade:
	helm upgrade tsmetrics deploy/helm/tsmetrics

helm-uninstall:
	helm uninstall tsmetrics

helm-template:
	helm template tsmetrics deploy/helm/tsmetrics

helm-lint:
	helm lint deploy/helm/tsmetrics

# ==============================================================================
# KUSTOMIZE
# ==============================================================================

.PHONY: kustomize-dev kustomize-prod kustomize-preview-dev kustomize-preview-prod
kustomize-dev:
	kubectl apply -k deploy/kustomize/overlays/development

kustomize-prod:
	kubectl apply -k deploy/kustomize/overlays/production

kustomize-preview-dev:
	kubectl kustomize deploy/kustomize/overlays/development

kustomize-preview-prod:
	kubectl kustomize deploy/kustomize/overlays/production

# ==============================================================================
# QUALITY & MAINTENANCE
# ==============================================================================

.PHONY: lint fmt deps
lint:
	golangci-lint run

fmt:
	go fmt ./...

deps:
	go mod tidy && go mod download

# ==============================================================================
# HELP
# ==============================================================================

.PHONY: help
help:
	@echo "TSMetrics Build Commands:"
	@echo ""
	@echo "Core:"
	@echo "  build          Build the binary"
	@echo "  test           Run tests"
	@echo "  clean          Remove build artifacts"
	@echo "  clean-port     Kill processes on port 9100"
	@echo "  clean-all      Full cleanup"
	@echo ""
	@echo "Development:"
	@echo "  dev            Start development with live reload"
	@echo "  run            Run directly with go run"
	@echo ""
	@echo "Docker:"
	@echo "  docker-build   Build Docker image"
	@echo "  docker-run     Run Docker container (uses .env file)"
	@echo ""
	@echo "Helm:"
	@echo "  helm-install   Install with Helm"
	@echo "  helm-upgrade   Upgrade with Helm"
	@echo "  helm-template  Preview Helm templates"
	@echo "  helm-lint      Lint Helm chart"
	@echo ""
	@echo "Kustomize:"
	@echo "  kustomize-dev     Deploy development"
	@echo "  kustomize-prod    Deploy production"
	@echo "  kustomize-preview-dev  Preview development"
	@echo "  kustomize-preview-prod Preview production"
	@echo ""
	@echo "Quality:"
	@echo "  lint           Run golangci-lint"
	@echo "  fmt            Format Go code"
	@echo "  deps           Update dependencies"
