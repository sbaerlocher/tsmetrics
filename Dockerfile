FROM golang:1.26-alpine@sha256:2389ebfa5b7f43eeafbd6be0c3700cc46690ef842ad962f6c5bd6be49ed82039 AS builder

# Add build arguments for version info
ARG VERSION=dev
ARG BUILD_TIME=unknown
ARG VERSION_LONG=""
ARG VERSION_SHORT=""

RUN apk update && \
  apk add --no-cache git~2.52

WORKDIR /src

COPY .git .git/

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
  -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME} -X tailscale.com/version.longStamp=${VERSION_LONG} -X tailscale.com/version.shortStamp=${VERSION_SHORT}" \
  -buildvcs=true \
  -buildmode=default \
  -o /tsmetrics ./cmd/tsmetrics

FROM scratch

# Add OCI metadata labels
LABEL org.opencontainers.image.title="tsmetrics"
LABEL org.opencontainers.image.description="A comprehensive Tailscale Prometheus exporter that combines API metadata with live device metrics for complete network observability."
LABEL org.opencontainers.image.source="https://github.com/sbaerlocher/tsmetrics"
LABEL org.opencontainers.image.licenses="MIT"

# Add ca-certificates for HTTPS calls
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /tsmetrics /tsmetrics

# Create non-root user
USER 65534:65534

EXPOSE 9100

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD ["/tsmetrics", "-health-check"]

ENTRYPOINT ["/tsmetrics"]
CMD []
