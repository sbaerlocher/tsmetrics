# Code Review Guidelines

## Scope

In scope:

- Go source changes (`cmd/`, `internal/`, `pkg/`)
- Dockerfile and build configuration
- Helm chart changes (`deploy/helm/`)
- CI/CD workflow changes
- Renovate configuration updates

Out of scope:

- Auto-generated files (`go.sum`, `dist/`)
- Renovate dependency-only PRs (patch/minor with automerge enabled)

## Required checks

- All tests pass (`go test ./...`)
- No secrets committed — all sensitive values via environment variables
- New metrics include proper labels and documentation
- Dockerfile follows existing multi-stage build pattern
- Helm chart values updated when new configuration is added

## Severity levels

| Level        | Meaning                                             | Merge impact       |
| ------------ | --------------------------------------------------- | ------------------ |
| Bug          | Incorrect behavior or broken contract               | Blocks merge       |
| Nit          | Minor issue — suboptimal but not incorrect          | Non-blocking       |
| Pre-existing | Issue present before this PR; flagged for awareness | No action required |

## Skip

- Renovate PRs with `automerge: true` (patch/minor) after CI passes
- Documentation-only changes with no functional impact
