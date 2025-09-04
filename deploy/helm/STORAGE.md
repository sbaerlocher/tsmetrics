# Storage Options for tsmetrics

The tsmetrics Helm chart supports multiple storage backends for the tsnet state directory. This allows you to choose the best storage solution for your specific use case.

## Available Storage Types

### 1. PersistentVolumeClaim (CSI) - Default
Best for: Production environments requiring persistent state across pod restarts
```yaml
storage:
  type: "pvc"
  pvc:
    enabled: true
    accessMode: ReadWriteOnce
    size: 1Gi
    storageClass: "fast-ssd"  # optional
```

### 2. EmptyDir
Best for: Development, testing, or when tsnet state doesn't need to persist across pod restarts
```yaml
storage:
  type: "emptyDir"
  emptyDir:
    sizeLimit: "1Gi"
    medium: ""  # "" for disk, "Memory" for tmpfs
```

### 3. HostPath
Best for: Single-node clusters or when you want node-local persistence
```yaml
storage:
  type: "hostPath"
  hostPath:
    path: "/var/lib/tsmetrics"
    type: "DirectoryOrCreate"
```

### 4. Memory (tmpfs)
Best for: Maximum performance when state persistence is not critical
```yaml
storage:
  type: "memory"
  memory:
    sizeLimit: "512Mi"
```

## Usage Examples

### Quick Start (No Persistence)
```bash
helm install tsmetrics ./deploy/helm --set storage.type=emptyDir
```

### Production with Custom Storage Class
```bash
helm install tsmetrics ./deploy/helm \
  --set storage.pvc.storageClass=fast-ssd \
  --set storage.pvc.size=5Gi
```

### High Performance Setup
```bash
helm install tsmetrics ./deploy/helm \
  --set storage.type=memory \
  --set storage.memory.sizeLimit=1Gi
```

### Node-Local Persistence
```bash
helm install tsmetrics ./deploy/helm \
  --set storage.type=hostPath \
  --set storage.hostPath.path=/opt/tsmetrics-state
```

## Storage Type Comparison

| Type | Persistence | Performance | Multi-Node | Use Case |
|------|-------------|-------------|------------|----------|
| pvc | ✅ | Medium | ✅ | Production |
| emptyDir | ⚠️ (Pod lifetime) | High | ✅ | Development |
| hostPath | ✅ (Node lifetime) | High | ❌ | Single-node |
| memory | ❌ | Highest | ✅ | Testing/Performance |

## Security Considerations

- **hostPath**: Requires appropriate pod security policies or admission controllers
- **pvc**: Depends on storage class security features
- **memory**: Limited by node memory and may be cleared by OOM killer
- **emptyDir**: Disk-based emptyDir inherits node security model
