import re

with open('internal/mihomo/gate1_legacy_test.go', 'r', encoding='utf-8') as f:
    text = f.read()

# First, modify ApplyCoordinatorHooks and checkpointManifestLocked if necessary.
# Let's check if checkpointManifestLocked respects hooks for bridge states.
