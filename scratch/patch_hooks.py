import re

with open('internal/mihomo/coordinator.go', 'r', encoding='utf-8') as f:
    text = f.read()

# Add FailManifestPersistBridgeOpState to ApplyCoordinatorHooks
hook_decl_old = '''type ApplyCoordinatorHooks struct {
\tFailManifestPersistAtState ManifestState
\tFailCommitVerifiedActive   bool
\tPostCommitHook             func()
}'''
hook_decl_new = '''type ApplyCoordinatorHooks struct {
\tFailManifestPersistAtState       ManifestState
\tFailCommitVerifiedActive         bool
\tPostCommitHook                   func()
\tFailManifestPersistBridgeOpState string
}'''
text = text.replace(hook_decl_old, hook_decl_new)

# Add hook check to casManifest
cas_old = '''\tif c.hooks.FailManifestPersistAtState == next.State {
\t\treturn fmt.Errorf("failpoint: manifest persist failed at state %s", next.State)
\t}'''
cas_new = '''\tif c.hooks.FailManifestPersistAtState == next.State && c.hooks.FailManifestPersistAtState != "" {
\t\treturn fmt.Errorf("failpoint: manifest persist failed at state %s", next.State)
\t}
\tif c.hooks.FailManifestPersistBridgeOpState != "" && len(next.BridgeOperations) > 0 {
\t\tlastOp := next.BridgeOperations[len(next.BridgeOperations)-1]
\t\tif string(lastOp.State) == c.hooks.FailManifestPersistBridgeOpState {
\t\t\tc.hooks.FailManifestPersistBridgeOpState = "" // only fail once
\t\t\treturn fmt.Errorf("failpoint: manifest persist failed at bridge state %s", lastOp.State)
\t\t}
\t}'''
text = text.replace(cas_old, cas_new)

with open('internal/mihomo/coordinator.go', 'w', encoding='utf-8') as f:
    f.write(text)
