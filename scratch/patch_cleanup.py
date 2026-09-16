import re

with open('scratch/coordinator.go', 'r', encoding='utf-8') as f:
    text = f.read()

cleanup_files_old = '''func (c *ApplyCoordinator) processCleanupJournalFilesLocked(cj *CleanupJournal) {
\tif cj == nil || len(cj.Files) == 0 {'''

cleanup_files_new = '''func (c *ApplyCoordinator) processCleanupJournalFilesLocked(cj *CleanupJournal) error {
\tif cj == nil || len(cj.Files) == 0 {
\t\tvar finalErr error
\t\tif err := strictfs.StrictUnlink(c.cleanupJournalFile); err != nil && !os.IsNotExist(err) {
\t\t\tfinalErr = fmt.Errorf("cleanup journal unlink failed: %w", err)
\t\t\t_ = c.writeRecoveryMarkerLocked(finalErr.Error())
\t\t}
\t\tif err := strictfs.StrictUnlink(c.manifestFile); err != nil && !os.IsNotExist(err) {
\t\t\tif finalErr == nil { finalErr = fmt.Errorf("manifest unlink failed: %w", err) }
\t\t\t_ = c.writeRecoveryMarkerLocked(err.Error())
\t\t}
\t\treturn finalErr
\t}

\tvar remaining []string
\tchanged := false
\tvar unlinkErrs []error
\tfor _, f := range cj.Files {
\t\ttargetPath := filepath.Join(c.cfg.ConfigDir, f)
\t\terr := strictfs.StrictUnlink(targetPath)
\t\tif err != nil && !os.IsNotExist(err) {
\t\t\tremaining = append(remaining, f)
\t\t\tunlinkErrs = append(unlinkErrs, fmt.Errorf("unlink %s: %w", f, err))
\t\t\t_ = c.writeRecoveryMarkerLocked(fmt.Sprintf("cleanup unlink failed %s: %v", f, err))
\t\t} else {
\t\t\tif _, statErr := os.Stat(targetPath); statErr == nil || !os.IsNotExist(statErr) {
\t\t\t\tremaining = append(remaining, f)
\t\t\t\tunlinkErrs = append(unlinkErrs, fmt.Errorf("verify unlink %s: %v", f, statErr))
\t\t\t\t_ = c.writeRecoveryMarkerLocked(fmt.Sprintf("cleanup unlink verify failed %s: stat %v", f, statErr))
\t\t\t} else {
\t\t\t\tchanged = true
\t\t\t}
\t\t}
\t}

\tvar finalErr error
\tif len(unlinkErrs) > 0 {
\t\tfinalErr = unlinkErrs[0]
\t}

\tif len(remaining) == 0 {
\t\tif err := strictfs.StrictUnlink(c.cleanupJournalFile); err != nil && !os.IsNotExist(err) {
\t\t\tfinalErr = fmt.Errorf("cleanup journal unlink failed: %w", err)
\t\t\t_ = c.writeRecoveryMarkerLocked(finalErr.Error())
\t\t}
\t\tif err := strictfs.StrictUnlink(c.manifestFile); err != nil && !os.IsNotExist(err) {
\t\t\tif finalErr == nil { finalErr = fmt.Errorf("manifest unlink failed: %w", err) }
\t\t\t_ = c.writeRecoveryMarkerLocked(err.Error())
\t\t}
\t} else if changed {
\t\tcj.Sequence++
\t\tcj.Files = remaining
\t\tif b, err := json.MarshalIndent(cj, "", "  "); err == nil {
\t\t\tif err := strictfs.StrictWriteAtomic(c.cleanupJournalFile, b, 0600); err != nil {
\t\t\t\t_ = c.writeRecoveryMarkerLocked(fmt.Sprintf("cleanup journal rewrite failed: %v", err))
\t\t\t}
\t\t}
\t}
\treturn finalErr
}
'''

def replace_func(text, func_name, new_impl):
    pattern = r'^func \(c \*ApplyCoordinator\) ' + func_name + r'\b.*?^}$'
    match = re.search(pattern, text, re.DOTALL | re.MULTILINE)
    if not match:
        print(f'Function {func_name} not found!')
        return text
    return text[:match.start()] + new_impl + text[match.end():]

text = replace_func(text, 'processCleanupJournalFilesLocked', cleanup_files_new)

cleanup_tx_new = '''func (c *ApplyCoordinator) cleanupTxArtifactsLocked(m *TransactionManifest) error {
\tvar files []string
\tif m.PreMutationStoreSnapshotFile != "" {
\t\tfiles = append(files, filepath.Base(m.PreMutationStoreSnapshotFile))
\t}
\tif m.CandidateConfigFile != "" {
\t\tfiles = append(files, filepath.Base(m.CandidateConfigFile))
\t}
\tif m.CandidatePostMutationStoreSnapshotFile != "" {
\t\tfiles = append(files, filepath.Base(m.CandidatePostMutationStoreSnapshotFile))
\t}

\tvar cj *CleanupJournal
\tif len(files) > 0 {
\t\tcj = &CleanupJournal{
\t\t\tVersion:  1,
\t\t\tSequence: 1,
\t\t\tTxID:     m.TxID,
\t\t\tFiles:    files,
\t\t}
\t\tif b, err := json.MarshalIndent(cj, "", "  "); err == nil {
\t\t\tif wErr := strictfs.StrictWriteAtomic(c.cleanupJournalFile, b, 0600); wErr != nil {
\t\t\t\t_ = c.writeRecoveryMarkerLocked(fmt.Sprintf("cleanup journal write failed: %v", wErr))
\t\t\t\treturn fmt.Errorf("cleanup journal write failed: %w", wErr)
\t\t\t}
\t\t}
\t}

\tif err := c.transitionManifestLocked(m, StateTerminalCleanup); err != nil {
\t\t_ = c.writeRecoveryMarkerLocked(fmt.Sprintf("terminal cleanup transition failed: %v", err))
\t\treturn fmt.Errorf("terminal cleanup transition failed: %w", err)
\t}

\treturn c.processCleanupJournalFilesLocked(cj)
}'''

text = replace_func(text, 'cleanupTxArtifactsLocked', cleanup_tx_new)

with open('scratch/coordinator_patched1.go', 'w', encoding='utf-8') as f:
    f.write(text)
