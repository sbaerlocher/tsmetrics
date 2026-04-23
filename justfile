set dotenv-load := true

version    := `git describe --tags --always --dirty 2>/dev/null || echo dev`
build_time := `date -u '+%Y-%m-%d_%H:%M:%S'`

default:
    @just --list

# Start dev container and tail logs
dev:
    dde project:up
    dde project:logs --tail=50

# Run the test suite in the container
test:
    dde project:exec -- go test -v ./...

# Build the binary in the container with version ldflags
build:
    dde project:exec -- go build \
        -ldflags "-X main.version={{ version }} -X main.buildTime={{ build_time }}" \
        -o bin/tsmetrics ./cmd/tsmetrics

# Lint in the container
lint:
    dde project:exec -- golangci-lint run

