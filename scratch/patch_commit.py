import re

with open('scratch/coordinator_patched1.go', 'r', encoding='utf-8') as f:
    text = f.read()

def replace_func(text, func_name, new_impl):
    pattern = r'^func \(c \*ApplyCoordinator\) ' + func_name + r'\b.*?^}'
    match = re.search(pattern, text, re.DOTALL | re.MULTILINE)
    if not match:
        print(f'Function {func_name} not found!')
        return text
    return text[:match.start()] + new_impl + text[match.end():]

commit_new = '''func (c *ApplyCoordinator) executeFinalCommitLocked(ctx context.Context, manifest *TransactionManifest, rec AppliedGenerationRecord) error {
\t// 1. require durable StateCommitIntent
\tif manifest.State != StateCommitIntent {
\t\treturn fmt.Errorf("invalid state for final commit: %s", manifest.State)
\t}

\t// 2. write verified-active
\tif c.hooks.FailCommitVerifiedActive {
\t\tc.setStateLocked(StateRecoveryRequired)
\t\t_ = c.writeRecoveryMarkerLocked("failpoint: commit verified active failed")
\t\treturn ErrRecoveryRequired
\t}

\trecBytes, err := json.MarshalIndent(rec, "", "  ")
\tif err != nil {
\t\tc.setStateLocked(StateRecoveryRequired)
\t\t_ = c.writeRecoveryMarkerLocked("marshal verified active: " + err.Error())
\t\treturn ErrRecoveryRequired
\t}

\tif err := strictfs.StrictWriteAtomic(c.verifiedActiveFile, recBytes, 0600); err != nil {
\t\tc.setStateLocked(StateRecoveryRequired)
\t\t_ = c.writeRecoveryMarkerLocked("write verified active: " + err.Error())
\t\treturn ErrRecoveryRequired
\t}

\t// 3. write current/LKG generation pointer
\tif err := c.genStore.AdvanceLKGPointer(rec.GenerationID, rec.Generation, rec, c.DaemonEpoch()); err != nil {
\t\tc.setStateLocked(StateRecoveryRequired)
\t\t_ = c.writeRecoveryMarkerLocked("update LKG pointer: " + err.Error())
\t\treturn ErrRecoveryRequired
\t}

\t// 4. read both, 5. verify
\tvaData, err := os.ReadFile(c.verifiedActiveFile)
\tif err != nil {
\t\tc.setStateLocked(StateRecoveryRequired)
\t\t_ = c.writeRecoveryMarkerLocked("verify verified-active read: " + err.Error())
\t\treturn ErrRecoveryRequired
\t}
\tvar verifyRec AppliedGenerationRecord
\tif err := json.Unmarshal(vaData, &verifyRec); err != nil {
\t\tc.setStateLocked(StateRecoveryRequired)
\t\t_ = c.writeRecoveryMarkerLocked("verify verified-active parse: " + err.Error())
\t\treturn ErrRecoveryRequired
\t}
\t
\t// Verified-Active Full Equality Check
\tif verifyRec.GenerationID != rec.GenerationID || 
\t   verifyRec.Generation != rec.Generation ||
\t   verifyRec.AppliedStoreDigest != rec.AppliedStoreDigest ||
\t   verifyRec.AppliedConfigDigest != rec.AppliedConfigDigest ||
\t   verifyRec.AppliedInputDigest != rec.AppliedInputDigest ||
\t   verifyRec.RuntimeMode != rec.RuntimeMode {
\t\tc.setStateLocked(StateRecoveryRequired)
\t\t_ = c.writeRecoveryMarkerLocked("verified-active full equality mismatch during commit verification")
\t\treturn ErrRecoveryRequired
\t}

\tptr, err := c.genStore.ReadLKGPointer()
\tif err != nil {
\t\tc.setStateLocked(StateRecoveryRequired)
\t\t_ = c.writeRecoveryMarkerLocked("verify LKG pointer read: " + err.Error())
\t\treturn ErrRecoveryRequired
\t}
\tif ptr.GenerationID != rec.GenerationID || ptr.GenerationNumber != rec.Generation || ptr.AppliedConfigDigest != rec.AppliedConfigDigest {
\t\tc.setStateLocked(StateRecoveryRequired)
\t\t_ = c.writeRecoveryMarkerLocked("LKG pointer generation mismatch during commit verification")
\t\treturn ErrRecoveryRequired
\t}

\tc.mu.Lock()
\tc.appliedRecord = &rec
\tc.mu.Unlock()

\t// 6. write StateCommitted
\tif tErr := c.transitionManifestLocked(manifest, StateCommitted); tErr != nil {
\t\tc.setStateLocked(StateRecoveryRequired)
\t\t_ = c.writeRecoveryMarkerLocked("transition to committed failed: " + tErr.Error())
\t\treturn fmt.Errorf("committed transition: %w", tErr)
\t}

\t// 7. terminal cleanup
\tif err := c.cleanupTxArtifactsLocked(manifest); err != nil {
\t\tc.log("error", "coordinator.commit", "commit succeeded with partial cleanup failure: " + err.Error())
\t\treturn fmt.Errorf("commit succeeded with partial cleanup failure: %w", err)
\t}

\tc.setStateLocked(StateIdle)
\treturn nil
}'''

text = replace_func(text, 'executeFinalCommitLocked', commit_new)

with open('scratch/coordinator_patched2.go', 'w', encoding='utf-8') as f:
    f.write(text)
