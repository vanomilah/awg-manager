#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Step 1: Run swag init strictly from cmd/awg-manager subshell (cwd-independent)
(
    cd "$REPO_ROOT/cmd/awg-manager"
    go run github.com/swaggo/swag/cmd/swag@v1.16.4 init \
        -g docs.go \
        -d .,../../internal/api,../../internal/aiassistant,../../internal/sys/routerinfo,../../internal/diagnostics,../../internal/presets,../../internal/sys/files,../../internal/sys/opkg,../../internal/sys/ports,../../internal/sys/procmon,../../internal/sys/services,../../internal/mihomonative \
        -o ../../internal/openapi \
        --parseInternal --ot yaml
)

# Step 2: Patch the generated YAML atomically
python3 "$REPO_ROOT/scripts/patch-openapi.py" "$REPO_ROOT/internal/openapi/swagger.yaml"
