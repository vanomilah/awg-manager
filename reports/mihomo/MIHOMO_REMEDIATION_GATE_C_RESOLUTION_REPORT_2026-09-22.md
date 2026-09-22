# Mihomo Remediation Gate C — Resolution Report (Rev. 2)

**Date:** 2026-09-22  
**Repository:** `E:\AWGM\awg-manager`  
**Branch:** `feature/mihomo-ai-proxyrt`  
**Task Specification:** `reports/mihomo/MIHOMO_GATE_C_ACCEPTANCE_REVIEW_REV2_2026-09-22.md`  
**Associated Patch:** `reports/mihomo/GATE_C_DIFF_2026-09-22.patch`  

---

## 1. Executive Summary & Verdict

**GATE C REMEDIATION REV. 2 COMPLETE — ALL ACCEPTANCE CONTRACTS VERIFIED.**

All findings, defects, and missing contracts identified in `reports/mihomo/MIHOMO_GATE_C_ACCEPTANCE_REVIEW_REV2_2026-09-22.md` have been implemented, tested, and verified against the authoritative codebase:

1. **P0-1 (`VerifyActiveProcessProof` missing receipt fail-closed):**
   - For `RuntimeEnforced`, an applied record with `ProcessReceipt == nil` fails closed, returning an error wrapping `ErrProcessProofFailed`.
   - For `RuntimeOff`, a nil receipt remains strictly valid (returns `nil`).
   - When no record is applied (`appliedRecord == nil`), returns `nil` (nothing active).
   - Authoritative OS verification (PID, start ticks, executable path, cmdline, listener socket ownership) is strictly performed whenever a receipt exists.
   - Regression: `TestGateC1_VerifyActiveProcessProof_NilReceiptFailsEnforced` in `internal/mihomo/gate_c_test.go`.

2. **P0-2 (Bridge compensation using genuinely observed before-image & live port):**
   - Extended `internal/singbox/proxy.go` with `ListenPort int` on `ProxyObservation`, `SetProxyPortGetter` on `ProxyManager`, and live running-config parsing for `proxy upstream 127.0.0.1 <port>`.
   - In `cmd/awg-manager/mihomo_bridge_runtime.go`, `inspectBridgeLocked` populates `BridgeRef.ListenPort`, `ProxyInterface`, and `KernelInterface` strictly from live NDMS `ProxyObservation`.
   - `ApplyBridges` verifies that modified bridges have genuinely observed `ListenPort > 0`. If `ListenPort <= 0` cannot be observed, it fails closed with `ErrVerificationFailed` before mutating any interface.
   - During compensation restore, `isAuthorizedCompensationRestoreLocked` authorizes restoring the genuine observed previous port even when it differs from the desired candidate port in the store.
   - Regression: `TestGateC4_ApplyBridges_ObservedBeforeImagePortRestoredOnBatchFailure` (both `ObservedPortRestoredOnBatchFailure` and `UnobservablePortRejectedBeforeMutation`) in `cmd/awg-manager/mihomo_bridge_gate_c_test.go`.

3. **P1-1 (Strict manifest decoding with legacy `process_receipt` compatibility):**
   - Added custom `UnmarshalJSON` to `GenerationManifest` in `internal/mihomo/types.go` that strictly rejects unknown fields via `json.Decoder.DisallowUnknownFields()` while accepting and discarding deprecated legacy `process_receipt`.
   - The legacy `process_receipt` is intentionally not stored in `GenerationManifest`, cannot be trusted as runtime evidence, and is never written out when new manifests are serialized.
   - All other unknown fields continue to be strictly rejected by `DecodeJSONStrict`.
   - Regression: `TestGateC3_GenerationManifest_LegacyReceiptCompatibility` in `internal/mihomo/gate_c_test.go`.

4. **P1-2 (Normal restart stop preserves `TxID` and surfaces marker write errors):**
   - Updated `restartControlledLocked` in `internal/mihomo/coordinator.go` to accept `txID string` and pass it down to `stopControlledLocked(ctx, txID, "restart")`.
   - Updated transactional callers (`applyTransactionLocked`, `rollbackToGenerationLocked`, `regenerate_from_desired`, and `ensureProcessProofLocked`) to pass `manifest.TxID` or `m.TxID`.
   - In `stopControlledLocked`, `writeRecoveryMarkerLocked` errors are now joined using `errors.Join` so durable marker write failures are surfaced.
   - In `handleApplyFailureLocked`, `writeRecoveryMarkerIfAbsentLocked` preserves the root-cause marker and includes `[TxID]` if writing fallback marker.
   - Regression: `TestGateC3_RestartStopFailure_PreservesTxIDInMarker` in `internal/mihomo/gate_c_test.go`.

---

## 2. Gate C File Allow-List (17 Files)

| # | File | Status | Gate C Scope & Remediation Summary |
|---|---|---|---|
| 1 | `cmd/awg-manager/mihomo_bridge_gate_c_test.go` | New File | P0-1, P0-2 regressions (`TestGateC4_ApplyBridges_ObservedBeforeImagePortRestoredOnBatchFailure`, `TestGateC4_ApplyBridges_PreExistingBridgePreservedOnBatchFailure`, `TestGateC4_InspectBridge_EmptySystemNameFailsClosed`). |
| 2 | `cmd/awg-manager/mihomo_bridge_runtime.go` | Modified | P0-2: Genuine live `ListenPort` observation, fail-closed on unobservable port, authorized compensation restore. |
| 3 | `cmd/awg-manager/mihomo_bridge_runtime_test.go` | Modified | Updated `fakeNDMSRegistrarWithOwnership` with live `ListenPort` support in `InspectProxy` and `ListProxyObservations`. |
| 4 | `internal/mihomo/coordinator.go` | Modified | P0-1: `VerifyActiveProcessProof` fail closed on nil receipt for `RuntimeEnforced`. P1-2: `restartControlledLocked` accepts `txID`, joins `writeRecoveryMarkerLocked` errors, preserves root-cause marker. |
| 5 | `internal/mihomo/gate2_test.go` | New File | Gate 2 regression suite with updated process verifier integration. |
| 6 | `internal/mihomo/gate4_test.go` | New File | Gate 4 regression suite with updated mock operator contracts. |
| 7 | `internal/mihomo/gate_c_linux_test.go` | New File | Linux-specific procfs verifier unit tests. |
| 8 | `internal/mihomo/gate_c_test.go` | New File | P0-1, P1-1, P1-2, and Gate C regression tests. |
| 9 | `internal/mihomo/generation_store.go` | Modified | Bundle manifest does not store `ProcessReceipt`. Verified-active retains authoritative receipt. |
| 10 | `internal/mihomo/operator.go` | Modified | Injected control seam (`SetProcessSeam`), bounded 5s reap, `done == nil` guard. |
| 11 | `internal/mihomo/process_verifier.go` | New File | Process verifier interface and no-op verifier. |
| 12 | `internal/mihomo/process_verifier_linux.go` | New File | Linux procfs implementation checking ticks, binary, and cmdline. |
| 13 | `internal/mihomo/process_verifier_other.go` | New File | Non-Linux compilation stubs. |
| 14 | `internal/mihomo/types.go` | Modified | P1-1: `UnmarshalJSON` on `GenerationManifest` with legacy `process_receipt` tolerance under `DisallowUnknownFields`. |
| 15 | `internal/singbox/proxy.go` | Modified | P0-2: `ProxyObservation.ListenPort`, `SetProxyPortGetter`, running-config upstream port parsing. |
| 16 | `internal/sys/procnet/listener.go` | Modified | P1-2: `ErrUnresolvedSocketOwner`, `ErrMalformedSocketTable`, unreadable fd tracking, IPv4/IPv6 wildcard separation, IPv4-only support. |
| 17 | `internal/sys/procnet/listener_test.go` | Modified | Regression tests for all 5 procnet contracts. |

---

## 3. Detailed Technical Remediation for Rev. 2 Findings

### 3.1 P0-1: `VerifyActiveProcessProof` Fail-Closed Semantics
In `internal/mihomo/coordinator.go`:
```go
func (c *ApplyCoordinator) VerifyActiveProcessProof(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.appliedRecord == nil {
		return nil
	}
	if c.appliedRecord.RuntimeMode == RuntimeOff {
		return nil
	}
	if c.appliedRecord.ProcessReceipt == nil {
		return fmt.Errorf("%w: enforced runtime requires authoritative process receipt", ErrProcessProofFailed)
	}
	receipt := c.appliedRecord.ProcessReceipt
	...
```
- Previously, `c.appliedRecord.ProcessReceipt == nil` returned `nil`, falsely treating an unverified runtime as valid.
- Now, `RuntimeEnforced` requires an authoritative `ProcessReceipt`, failing closed with `ErrProcessProofFailed` if missing.
- `RuntimeOff` and uninitialized runtime (`appliedRecord == nil`) return `nil`.
- When a receipt is present, strict OS procfs and listener ownership verification is performed.

### 3.2 P0-2: Bridge Compensation Before-Image & Live Port Observation
1. In `internal/singbox/proxy.go`:
   - Added `ListenPort int` to `ProxyObservation`.
   - Added `SetProxyPortGetter(fn func(context.Context, int) (int, error))` to `ProxyManager`.
   - Added `parseProxyPortFromRunningConfig` to extract `proxy upstream 127.0.0.1 <port>` from NDMS running-config lines when direct query is unavailable.
   - Populated `ListenPort` in `InspectProxy` and `ListProxyObservations`.
2. In `cmd/awg-manager/mihomo_bridge_runtime.go`:
   - `inspectBridgeLocked` builds `obsRef` purely from live NDMS `obsProxy`, setting `ListenPort: obsProxy.ListenPort`, `ProxyInterface: obsProxy.Name`, `KernelInterface: obsProxy.SystemName`.
   - In `ApplyBridges`, if `!isIdentical`:
     - If `obs.BridgeRef.ListenPort <= 0`, mutation is rejected before modifying any interface: `fmt.Errorf("%w: cannot safely modify bridge %s: live listen port cannot be observed for before-image compensation", mihomo.ErrVerificationFailed, ref.SlotKey())`.
     - If `obs.BridgeRef.ListenPort > 0`, genuine `beforeRef := obs.BridgeRef` is saved for `restore` compensation.
   - In `resolvePublishIdentity`, compensation restores are authorized via `isAuthorizedCompensationRestoreLocked` so NDMS receives the genuine old port even if store has the candidate port.

### 3.3 P1-1: Backward-Compatible Manifest Strict JSON Decoding
In `internal/mihomo/types.go`:
```go
func (gm *GenerationManifest) UnmarshalJSON(data []byte) error {
	if gm == nil {
		return fmt.Errorf("unmarshal into nil *GenerationManifest")
	}
	type Alias GenerationManifest
	aux := struct {
		*Alias
		DeprecatedProcessReceipt *json.RawMessage `json:"process_receipt,omitempty"`
	}{
		Alias: (*Alias)(gm),
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&aux); err != nil {
		return err
	}
	var dummy struct{}
	if err := dec.Decode(&dummy); err != io.EOF {
		return fmt.Errorf("unexpected trailing data in JSON payload")
	}
	return nil
}
```
- Existing generation bundles containing legacy `process_receipt` are cleanly parsed.
- `DeprecatedProcessReceipt` is discarded and not copied to `gm`.
- All other unknown fields continue to fail under `dec.DisallowUnknownFields()`.
- Newly marshaled manifests do not include `process_receipt` because `GenerationManifest` struct does not define it.

### 3.4 P1-2: Restart Stop Preserves Transaction Identity
1. In `internal/mihomo/coordinator.go`:
   - `restartControlledLocked(ctx context.Context, txID string, targetGeneration uint64, expectedDigest string, listeners []ListenerSpec) error`:
     Calls `c.stopControlledLocked(ctx, txID, "restart")`.
   - Transactional callers (`applyTransactionLocked`, `rollbackToGenerationLocked`, `regenerate_from_desired`, and `ensureProcessProofLocked`) pass their active `TxID`.
   - In `stopControlledLocked`:
     ```go
     if err := c.cfg.Operator.StopAndWait(ctx); err != nil {
         markerMsg := fmt.Sprintf("%s: stop operator failed: %v", reason, err)
         if txID != "" {
             markerMsg = fmt.Sprintf("[%s] %s", txID, markerMsg)
         }
         c.setState(StateRecoveryRequired)
         writeErr := c.writeRecoveryMarkerLocked(markerMsg)
         return errors.Join(fmt.Errorf("%s: %w", reason, err), writeErr)
     }
     ```
   - In `handleApplyFailureLocked`, `writeRecoveryMarkerIfAbsentLocked` preserves the root-cause marker and prefixes `[TxID]` if writing fallback marker.

---

## 4. Test Suite Execution & Verification Evidence

All test suites were executed in WSL Ubuntu (Linux x86_64) against the current working tree on 2026-09-22:

### 4.1 Full Package Unit Suite
```bash
go test -count=1 ./internal/sys/procnet ./internal/mihomo ./internal/singbox ./internal/mihomonative ./cmd/awg-manager
```
Output:
```text
ok  	github.com/hoaxisr/awg-manager/internal/sys/procnet	0.023s
ok  	github.com/hoaxisr/awg-manager/internal/mihomo	34.169s
ok  	github.com/hoaxisr/awg-manager/internal/singbox	25.134s
ok  	github.com/hoaxisr/awg-manager/internal/mihomonative	0.042s
ok  	github.com/hoaxisr/awg-manager/cmd/awg-manager	0.323s
```

### 4.2 Full Package Race Detection Suite
```bash
go test -race -count=1 ./internal/sys/procnet ./internal/mihomo ./internal/singbox ./cmd/awg-manager
```
Output:
```text
ok  	github.com/hoaxisr/awg-manager/internal/sys/procnet	1.038s
ok  	github.com/hoaxisr/awg-manager/internal/mihomo	38.886s
ok  	github.com/hoaxisr/awg-manager/internal/singbox	26.803s
ok  	github.com/hoaxisr/awg-manager/cmd/awg-manager	1.904s
```

### 4.3 Rev. 2 Specific Regression Tests

#### P0-1 Regression:
```bash
go test -v -run TestGateC1_VerifyActiveProcessProof_NilReceiptFailsEnforced ./internal/mihomo
```
Output:
```text
=== RUN   TestGateC1_VerifyActiveProcessProof_NilReceiptFailsEnforced
--- PASS: TestGateC1_VerifyActiveProcessProof_NilReceiptFailsEnforced (0.00s)
PASS
ok  	github.com/hoaxisr/awg-manager/internal/mihomo	0.010s
```

#### P0-2 Regression:
```bash
go test -v -run TestGateC4_ApplyBridges_ObservedBeforeImagePortRestoredOnBatchFailure ./cmd/awg-manager
```
Output:
```text
=== RUN   TestGateC4_ApplyBridges_ObservedBeforeImagePortRestoredOnBatchFailure
=== RUN   TestGateC4_ApplyBridges_ObservedBeforeImagePortRestoredOnBatchFailure/ObservedPortRestoredOnBatchFailure
=== RUN   TestGateC4_ApplyBridges_ObservedBeforeImagePortRestoredOnBatchFailure/UnobservablePortRejectedBeforeMutation
--- PASS: TestGateC4_ApplyBridges_ObservedBeforeImagePortRestoredOnBatchFailure (0.00s)
    --- PASS: TestGateC4_ApplyBridges_ObservedBeforeImagePortRestoredOnBatchFailure/ObservedPortRestoredOnBatchFailure (0.00s)
    --- PASS: TestGateC4_ApplyBridges_ObservedBeforeImagePortRestoredOnBatchFailure/UnobservablePortRejectedBeforeMutation (0.00s)
PASS
ok  	github.com/hoaxisr/awg-manager/cmd/awg-manager	0.016s
```

#### P1-1 Regression:
```bash
go test -v -run TestGateC3_GenerationManifest_LegacyReceiptCompatibility ./internal/mihomo
```
Output:
```text
=== RUN   TestGateC3_GenerationManifest_LegacyReceiptCompatibility
--- PASS: TestGateC3_GenerationManifest_LegacyReceiptCompatibility (0.00s)
PASS
ok  	github.com/hoaxisr/awg-manager/internal/mihomo	0.011s
```

#### P1-2 Regression:
```bash
go test -v -run TestGateC3_RestartStopFailure_PreservesTxIDInMarker ./internal/mihomo
```
Output:
```text
=== RUN   TestGateC3_RestartStopFailure_PreservesTxIDInMarker
--- PASS: TestGateC3_RestartStopFailure_PreservesTxIDInMarker (0.00s)
PASS
ok  	github.com/hoaxisr/awg-manager/internal/mihomo	0.012s
```

### 4.4 Formatting & Diff Cleanliness
```bash
gofmt -w internal/mihomo internal/sys/procnet internal/singbox cmd/awg-manager
git diff --check -- cmd/awg-manager internal/mihomo internal/singbox internal/sys/procnet
```
Output: Exit code 0, clean formatting and zero trailing whitespace.

### 4.5 Patch Application & Reversibility
```bash
git apply --check --reverse reports/mihomo/GATE_C_DIFF_2026-09-22.patch
```
Output: Exit code 0, patch applies and reverses cleanly without conflicts.

---

## 5. Strict Project Rules & Constraints Compliance

1. **Rule 1: Ban on `--force-reinstall`:** Strictly obeyed. Zero occurrences generated, referenced, or executed.
2. **Rule 2: Ban on `--cleanup`:** Strictly obeyed. Zero occurrences executed automatically.
3. **Rule 3: Workspace Integrity:** All work performed in `e:\AWGM\awg-manager` on branch `feature/mihomo-ai-proxyrt`. Directory `awg-manager-mihomo` was untouched.
4. **Rule 4: Router & Routing Protection:** Router IP `192.168.90.1` was not contacted; no package was built or deployed. Inbound mixed port `1099`, interface `Wireguard2`, and `telemt` `0.0.0.0:8443` remain intact.
