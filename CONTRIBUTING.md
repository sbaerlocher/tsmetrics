# Contributing to tsmetrics

Thank you for your interest in contributing! This document covers the
essentials. For the full build, test, and runtime reference see
[`AGENTS.md`](AGENTS.md).

## Prerequisites

- Go >= 1.26
- Git
- [dde](https://dde.sh) (Docker dev environment) for the containerized workflow
- Optional: [lefthook](https://lefthook.dev) for local git hooks

## Development Setup

### 1. Fork and Clone

```bash
git clone https://github.com/YOUR_USERNAME/tsmetrics.git
cd tsmetrics
git remote add upstream https://github.com/sbaerlocher/tsmetrics.git
cp .env.example .env   # then edit with your credentials (or mock values)
```

### 2. Create a Branch

```bash
git checkout -b feat/your-feature-name
# or
git checkout -b fix/issue-description
```

### 3. Run the Dev Loop

```bash
just dev      # start dev container with live reload + logs
just test     # run the test suite in the container
just lint     # run golangci-lint
just build    # build the binary with version ldflags
```

## Coding Standards

- **Formatter:** `gofmt` (run on save; lefthook auto-formats staged files).
- **Linter:** `golangci-lint`, config in `.golangci.yml`. Fix all findings.
- **Line length:** 120 characters.
- Match the surrounding code's naming, comment density, and idiom.

## Submitting Changes

### Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/). Sign and
sign-off your commits:

```bash
git commit -S --signoff -m "feat: add device latency metric"
```

### Pull Request Process

1. Ensure your branch is up to date with `main`.
2. Run `just lint` and `just test`; fix all issues.
3. Update `CHANGELOG.md` under `[Unreleased]`.
4. Open a PR and fill out the template; link related issues.
5. All CI checks must pass before merge.

## Getting Help

- **Issues:** Use GitHub issues for bugs and feature requests.
- **Reference:** [`AGENTS.md`](AGENTS.md) documents the full architecture,
  environment variables, and deployment paths.
