FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /tsmetrics .

FROM scratch
COPY --from=builder /tsmetrics /tsmetrics
EXPOSE 9100
ENTRYPOINT ["/tsmetrics"]
