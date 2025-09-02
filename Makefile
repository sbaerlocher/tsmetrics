# Makefile für tsmetrics

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
	@echo "Starting with Tailscale tsnet for development"
	@$(MAKE) dev

.PHONY: run-tsnet
run-tsnet: build
	@echo "Running tsmetrics with tsnet locally"
	@USE_TSNET=true TSNET_HOSTNAME=${TSNET_HOSTNAME:-tsmetrics-dev} TSNET_TAGS=${TSNET_TAGS:-exporter} REQUIRE_EXPORTER_TAG=${REQUIRE_EXPORTER_TAG:-true} TARGET_DEVICES=${TARGET_DEVICES:-gateway-140207,gateway-130104} ENV=${ENV:-development} PORT=${PORT:-9100} ./bin/$(APP_NAME)

.PHONY: docker-run-tsnet
docker-run-tsnet:
	docker run --rm -it \
		-e USE_TSNET=true \
		-e TSNET_HOSTNAME=tsmetrics \
		-e TSNET_TAGS=${TSNET_TAGS:-exporter} \
		-e REQUIRE_EXPORTER_TAG=${REQUIRE_EXPORTER_TAG:-true} \
		-e TARGET_DEVICES=${TARGET_DEVICES:-gateway-140207,gateway-130104} \
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
	@echo "🚀 Starting development environment"
	@if command -v air >/dev/null 2>&1; then \
		echo "📝 Using air for live reload"; \
		air -c .air.toml; \
	else \
		echo "📦 Installing air..."; \
		go install github.com/air-verse/air@latest; \
		air -c .air.toml; \
	fi

.PHONY: dev-direct
dev-direct:
	@echo "🚀 Starting development environment with go run"
	@go run main.go

.PHONY: release
release: clean test build docker-build docker-push

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build       - Build the binary"
	@echo "  test        - Run tests"
	@echo "  run         - Run locally"
	@echo "  docker-build - Build Docker image"
	@echo "  k8s-deploy  - Deploy to Kubernetes"
	@echo "  dev         - Format, test, and build"
	@echo "  release     - Full release pipeline"
