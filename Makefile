# Makefile für tsmetrics
#
# Environment Variables:
# - Copy .env.example to .env and configure your values
# - All development targets (dev, dev-direct, run-tsnet) automatically export environment variables
# - Required for operation: OAUTH_CLIENT_ID, OAUTH_CLIENT_SECRET, TAILNET_NAME
# - Optional: TARGET_DEVICES (defaults to sample devices)
# - For production: Set ENV=production, USE_TSNET=false (unless needed)
#
# Quick Start:
#   1. cp .env.example .env
#   2. Edit .env with your Tailscale OAuth credentials
#   3. make dev  # Starts development server with live reload

APP_NAME := tsmetrics
VERSION := $(shell git describe --tags --always --dirty)
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
DOCKER_IMAGE := ghcr.io/sbaerlocher/$(APP_NAME)

# Go Build Targets
.PHONY: build
build:
	go build -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)" -o bin/$(APP_NAME) .

.PHONY: test
test:
	go test -v ./...

.PHONY: run
run:
	go run main.go

.PHONY: clean
clean:
	rm -rf bin/

.PHONY: clean-port
clean-port:
	@echo "🧹 Cleaning up port 9100..."
	@lsof -ti:9100 | xargs -r kill -9 || echo "No process found on port 9100"
	@echo "✅ Port 9100 is now free"

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
	@./dev.sh

.PHONY: run-tsnet
run-tsnet: build
	@echo "Running tsmetrics with tsnet locally"
	@USE_TSNET=true \
	TSNET_HOSTNAME=${TSNET_HOSTNAME:-tsmetrics-dev} \
	TSNET_STATE_DIR=${TSNET_STATE_DIR:-/tmp/tsnet-state} \
	TSNET_TAGS=${TSNET_TAGS:-exporter} \
	REQUIRE_EXPORTER_TAG=${REQUIRE_EXPORTER_TAG:-true} \
	TARGET_DEVICES=${TARGET_DEVICES:-gateway-140207,gateway-130104} \
	ENV=${ENV:-development} \
	PORT=${PORT:-9100} \
	LOG_LEVEL=${LOG_LEVEL:-info} \
	LOG_FORMAT=${LOG_FORMAT:-text} \
	CLIENT_METRICS_TIMEOUT=${CLIENT_METRICS_TIMEOUT:-10s} \
	MAX_CONCURRENT_SCRAPES=${MAX_CONCURRENT_SCRAPES:-10} \
	CLIENT_METRICS_PORT=${CLIENT_METRICS_PORT:-5252} \
	OAUTH_CLIENT_ID=${OAUTH_CLIENT_ID} \
	OAUTH_CLIENT_SECRET=${OAUTH_CLIENT_SECRET} \
	TAILNET_NAME=${TAILNET_NAME} \
	VERSION=${VERSION} \
	BUILD_TIME=${BUILD_TIME} \
	./bin/$(APP_NAME)

.PHONY: docker-run-tsnet
docker-run-tsnet:
	docker run --rm -it \
		-e USE_TSNET=true \
		-e TSNET_HOSTNAME=tsmetrics \
		-e TSNET_STATE_DIR=/tmp/tsnet-state \
		-e TSNET_TAGS=${TSNET_TAGS:-exporter} \
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

# Kubernetes Targets
.PHONY: k8s-deploy
k8s-deploy:
	kubectl apply -f deploy/kubernetes.yaml

.PHONY: k8s-delete
k8s-delete:
	kubectl delete -f deploy/kubernetes.yaml

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

# All-in-one targets
.PHONY: dev
dev:
	@./dev.sh

.PHONY: dev-direct
dev-direct:
	@echo "🚀 Starting development environment with go run"
	@USE_TSNET=true \
	TSNET_HOSTNAME=${TSNET_HOSTNAME:-tsmetrics-dev} \
	TSNET_STATE_DIR=${TSNET_STATE_DIR:-/tmp/tsnet-state} \
	TSNET_TAGS=${TSNET_TAGS:-exporter} \
	REQUIRE_EXPORTER_TAG=${REQUIRE_EXPORTER_TAG:-true} \
	TARGET_DEVICES=${TARGET_DEVICES:-gateway-140207,gateway-130104} \
	ENV=${ENV:-development} \
	PORT=${PORT:-9100} \
	LOG_LEVEL=${LOG_LEVEL:-info} \
	LOG_FORMAT=${LOG_FORMAT:-text} \
	CLIENT_METRICS_TIMEOUT=${CLIENT_METRICS_TIMEOUT:-10s} \
	MAX_CONCURRENT_SCRAPES=${MAX_CONCURRENT_SCRAPES:-10} \
	CLIENT_METRICS_PORT=${CLIENT_METRICS_PORT:-5252} \
	OAUTH_CLIENT_ID=${OAUTH_CLIENT_ID} \
	OAUTH_CLIENT_SECRET=${OAUTH_CLIENT_SECRET} \
	TAILNET_NAME=${TAILNET_NAME} \
	VERSION=${VERSION} \
	BUILD_TIME=${BUILD_TIME} \
	go run .

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
	@echo ""
	@echo "Development targets:"
	@echo "  dev            - Start development with air live reload (exports all env vars)"
	@echo "  dev-direct     - Start development with go run (exports all env vars)"
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
	@echo "  k8s-deploy     - Deploy to Kubernetes"
	@echo "  k8s-delete     - Remove from Kubernetes"
	@echo ""
	@echo "Quality targets:"
	@echo "  lint           - Run golangci-lint"
	@echo "  fmt            - Format Go code"
	@echo "  dev-deps       - Install development dependencies"
	@echo ""
	@echo "  release        - Full release pipeline (clean, test, build, docker)"
