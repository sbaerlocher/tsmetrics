# ==============================================================================
# tsmetrics - Multi-Stage Dockerfile
# ==============================================================================
# Targets:
#   - backend-dev: Go exporter with Air hot reload + golangci-lint (for dde)
#   - production:  Minimal image with GitHub release binary (used by CI)
# ==============================================================================

# ==============================================================================
# BACKEND-DEV STAGE (Go + Air hot reload + golangci-lint)
# ==============================================================================
FROM golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS backend-dev

RUN apk add --no-cache git build-base curl ca-certificates

WORKDIR /app

ENV PORT=9100 \
    CGO_ENABLED=0

# renovate: datasource=go depName=github.com/air-verse/air
RUN go install github.com/air-verse/air@v1.67.4

# renovate: datasource=go depName=github.com/golangci/golangci-lint/v2/cmd/golangci-lint
RUN go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY .air.toml ./

RUN mkdir -p /app/tmp /app/bin /tmp/tsnet-tsmetrics

EXPOSE 9100

CMD ["/go/bin/air", "-c", ".air.toml"]

# ==============================================================================
# RELEASE BUILDER STAGE (Download GitHub Release Binary)
# ==============================================================================
FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS release-builder

ARG TARGETARCH
ARG VERSION

RUN apk add --no-cache curl tar ca-certificates && \
    ARCH=$(case ${TARGETARCH} in \
    amd64) echo "x86_64" ;; \
    arm64) echo "arm64" ;; \
    *) echo ${TARGETARCH} ;; \
    esac) && \
    curl -fsSL "https://github.com/sbaerlocher/tsmetrics/releases/download/${VERSION}/tsmetrics_Linux_${ARCH}.tar.gz" -o /tmp/tsmetrics.tar.gz && \
    tar -xzf /tmp/tsmetrics.tar.gz -C /tmp tsmetrics && \
    chmod +x /tmp/tsmetrics

# ==============================================================================
# PRODUCTION STAGE (GitHub Release Binary)
# ==============================================================================
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS production

LABEL org.opencontainers.image.title="tsmetrics"
LABEL org.opencontainers.image.description="A comprehensive Tailscale Prometheus exporter that combines API metadata with live device metrics for complete network observability."
LABEL org.opencontainers.image.source="https://github.com/sbaerlocher/tsmetrics"
LABEL org.opencontainers.image.licenses="MIT"

COPY --from=release-builder --chown=nonroot:nonroot /tmp/tsmetrics /tsmetrics

USER nonroot:nonroot

EXPOSE 9100

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD ["/tsmetrics", "-health-check"]

ENTRYPOINT ["/tsmetrics"]
