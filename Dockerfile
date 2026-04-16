# ==============================================================================
# tsmetrics - Dockerfile (GitHub Release Binary)
# ==============================================================================

# ==============================================================================
# RELEASE BUILDER STAGE (Download GitHub Release Binary)
# ==============================================================================
FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS release-builder

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
FROM gcr.io/distroless/static-debian12:nonroot@sha256:a9329520abc449e3b14d5bc3a6ffae065bdde0f02667fa10880c49b35c109fd1 AS production

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
