# Scripts Directory

This directory contains build and development scripts for tsmetrics.

## Scripts Overview

### `env-config.sh`

Central environment variable configuration:

- Loads default development settings
- Can be sourced by other scripts
- Handles build metadata (VERSION, BUILD_TIME)

### `dev.sh`

Development environment setup:

- Loads `.env` file if present
- Sets up development defaults
- Installs and runs air for live reload
- Provides colored logging and error handling

### `build.sh`

Build script for the application:

- Uses environment configuration
- Builds with proper version and build time metadata
- Cross-platform compatible

## Usage

```bash
# Start development environment
make dev                    # Uses scripts/dev.sh

# Build application
make build                  # Uses scripts/build.sh

# Load environment manually
source scripts/env-config.sh
```

## Best Practices

1. **Modular**: Each script has a single responsibility
2. **Reusable**: Scripts can be sourced by other scripts
3. **Error Handling**: Proper error handling with `set -euo pipefail`
4. **Logging**: Consistent colored logging functions
5. **Documentation**: Clear function and variable names

## Environment Variables

All environment variables are centrally managed in `env-config.sh`.
Default values are provided for development, but can be overridden via:

1. `.env` file in project root
2. System environment variables
3. Makefile variables (for build metadata)
