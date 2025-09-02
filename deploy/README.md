# Kubernetes Deployment Options

This directory contains multiple deployment options for tsmetrics:

## Plain Kubernetes (deprecated)

- `kubernetes.yaml` - Simple deployment with basic configuration

## Helm Chart

Location: `helm/tsmetrics/`

### Features
- Configurable values via `values.yaml`
- Secret management
- Resource limits and requests
- Health checks
- Optional persistence

### Installation

```bash
# Install with default values
helm install tsmetrics ./helm/tsmetrics

# Install with custom values
helm install tsmetrics ./helm/tsmetrics -f my-values.yaml

# Upgrade
helm upgrade tsmetrics ./helm/tsmetrics
```

### Configuration

#### Helm with External Secrets

```yaml
# values.yaml
externalSecret:
  enabled: true
  secretName: "my-tailscale-secrets"

serviceMonitor:
  enabled: true
  interval: 30s
```

```bash
# Create external secret first
kubectl create secret generic my-tailscale-secrets \
  --from-literal=OAUTH_CLIENT_ID=your-id \
  --from-literal=OAUTH_CLIENT_SECRET=your-secret \
  --from-literal=TAILNET_NAME=your-company

# Install chart
helm install tsmetrics ./helm/tsmetrics -f values.yaml
```

#### Kustomize with Custom Secret

```bash
# Create secret
kubectl create secret generic tsmetrics-secrets \
  --from-literal=OAUTH_CLIENT_ID=your-id \
  --from-literal=OAUTH_CLIENT_SECRET=your-secret \
  --from-literal=TAILNET_NAME=your-company

# Development (no ServiceMonitor)
kubectl apply -k kustomize/overlays/development

# Production (with ServiceMonitor)
kubectl apply -k kustomize/overlays/production
```

### Features Comparison

| Feature | Helm | Kustomize Base | Kustomize Dev | Kustomize Prod |
|---------|------|----------------|---------------|----------------|
| ServiceMonitor | Optional | ❌ | ❌ | ✅ |
| External Secrets | ✅ | ✅ | ✅ | ✅ |
| HPA | Optional | ❌ | ❌ | ✅ |
| Persistence | Optional | ❌ | ❌ | ✅ |
| Resource Limits | Configurable | Basic | Reduced | Production |

## Kustomize

Location: `kustomize/`

### Structure
- `base/` - Base resources
- `overlays/development/` - Development configuration
- `overlays/production/` - Production configuration

### Features
- Environment-specific configurations
- Strategic merge patches
- ConfigMap generation
- Image tag management
- Resource scaling

### Usage

```bash
# Deploy development environment
kubectl apply -k kustomize/overlays/development

# Deploy production environment
kubectl apply -k kustomize/overlays/production

# Preview changes
kubectl kustomize kustomize/overlays/production
```

### Configuration

1. Create a secret with your Tailscale credentials:

```bash
kubectl create secret generic tsmetrics-secrets \
  --from-literal=OAUTH_CLIENT_ID=your-client-id \
  --from-literal=OAUTH_CLIENT_SECRET=your-client-secret \
  --from-literal=TAILNET_NAME=your-company
```

2. Apply the configuration:

```bash
kubectl apply -k kustomize/overlays/production
```

## Comparison

| Feature | Plain YAML | Helm | Kustomize |
|---------|------------|------|-----------|
| Templating | ❌ | ✅ | ❌ |
| Values Management | ❌ | ✅ | ❌ |
| Environment Overlays | ❌ | ❌ | ✅ |
| Patch Management | ❌ | ❌ | ✅ |
| Package Management | ❌ | ✅ | ❌ |
| Version Control | ✅ | ✅ | ✅ |
| Learning Curve | Low | Medium | Medium |

## Recommendations

- **Development**: Use Kustomize overlays for quick iterations
- **Production**: Use Helm for better lifecycle management
- **Simple Deployments**: Use plain YAML for basic setups
- **Multi-Environment**: Use Kustomize for environment-specific configurations

## Examples

### Helm with Custom Values

```yaml
# my-values.yaml
image:
  tag: "v2.0.0"

tailscale:
  oauthClientId: "k123abc..."
  oauthClientSecret: "tskey-client-..."
  tailnetName: "company.ts.net"

resources:
  requests:
    cpu: 200m
    memory: 256Mi
  limits:
    cpu: 1000m
    memory: 1Gi

persistence:
  enabled: true
  size: 2Gi
```

### Kustomize Production Override

```yaml
# kustomize/overlays/production/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - ../../base

replicas:
  - name: tsmetrics
    count: 3

images:
  - name: ghcr.io/sbaerlocher/tsmetrics
    newTag: v2.0.0
```
