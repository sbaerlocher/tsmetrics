# VS Code Konfiguration für tsmetrics

Dieses Projekt ist für optimale Entwicklung mit VS Code konfiguriert.

## Installierte Extensions

Die folgenden Extensions werden automatisch empfohlen und sollten installiert werden:

- **Kubernetes Tools** (`ms-kubernetes-tools.vscode-kubernetes-tools`)
- **YAML Language Support** (`redhat.vscode-yaml`)
- **Helm Intellisense** (`tim-koehler.helm-intellisense`)
- **Go** (`golang.go`)
- **Makefile Tools** (`ms-vscode.makefile-tools`)

## Datei-Erkennung

- **Helm Templates** (`deploy/helm/*/templates/*.yaml`) → Helm-Syntax, keine YAML-Lint-Fehler
- **Kustomize Files** (`deploy/kustomize/**/*.yaml`) → YAML mit Kubernetes-Schema
- **Chart.yaml/values.yaml** → Standard YAML mit Validierung
- **Go Files** → Vollständige Go-Entwicklungsumgebung

## Verfügbare Tasks

Über Command Palette (Cmd+Shift+P) → "Tasks: Run Task":

### Build Tasks

- **Go Build** - Kompiliert die Anwendung
- **Helm Template** - Rendert Helm-Templates
- **Kustomize Build** - Baut Kustomize-Overlays

### Test Tasks

- **Go Test** - Führt Go-Tests aus
- **Helm Lint** - Validiert Helm-Charts

## Debugging

Konfiguriertes Debugging für:

- Go-Anwendung mit Entwicklungsumgebung
- Go-Tests

## Einstellungen

### YAML/Helm

- 2-Space-Einrückung
- Helm-Template-Unterstützung ohne Lint-Fehler
- Kubernetes-Schema-Validierung für normale YAML-Dateien

### Go

- Standard Go-Entwicklungsumgebung
- Automatische Imports
- Code-Formatierung

## Workspace öffnen

```bash
code tsmetrics.code-workspace
```

Oder direkt:

```bash
code .
```

Die Konfiguration wird automatisch geladen.
