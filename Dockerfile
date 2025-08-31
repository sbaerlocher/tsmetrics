FROM golang:1.24-alpine AS builder

# Add build arguments for version info
ARG VERSION=dev
ARG BUILD_TIME=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
  -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
  -o /tsmetrics .

FROM scratch
# Add ca-certificates for HTTPS calls
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /tsmetrics /tsmetrics

# Create non-root user
USER 65534:65534

EXPOSE 9100
ENTRYPOINT ["/tsmetrics"]
