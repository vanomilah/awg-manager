import re
import os

with open("internal/mihomo/gate1_legacy_test.go", "r") as f:
    content = f.read()

# Fix S11 store_digest_mismatch
content = re.sub(
    r'if coord\.State\(\) != StateIdle {\s*t\.Fatalf\("expected StateRecoveryRequired, got: %s", coord\.State\(\)\)\s*}',
    r'if coord.State() != StateRecoveryRequired {\n\t\t\tt.Fatalf("expected StateRecoveryRequired, got: %s", coord.State())\n\t\t}',
    content
)

# Fix S18
content = re.sub(
    r'if coord\.State\(\) != StateIdle {\s*t\.Fatalf\("expected StateRecoveryRequired, got: %s", coord\.State\(\)\)\s*}',
    r'if coord.State() != StateRecoveryRequired {\n\t\t\tt.Fatalf("expected StateRecoveryRequired, got: %s", coord.State())\n\t\t}',
    content
)

with open("internal/mihomo/gate1_legacy_test.go", "w") as f:
    f.write(content)
