# Mihomo Gate E — re-acceptance review

**Date:** 2026-09-22  
**Patch SHA-256:** `3AE00896B39F3503D7C656C4C99C48986BD55F5AC4A0C992258274AB1EB4DD81`  
**Resolution report SHA-256:** `A0EF8A932B0BA5372C5D8D91B43D29C374DDDBEE3933E048520F010DD461E5E4`

## Verdict

**Gate E remains conditionally rejected: two P1 defects remain.**

The revision correctly fixes most findings from `MIHOMO_GATE_E_ACCEPTANCE_REVIEW_2026-09-22.md`:

- parked ownership is no longer deleted merely because the edited configuration is non-conflicting;
- parking state now has centralized semantic validation on load and save;
- Bearer/Authorization masking covers the previously leaking token punctuation cases;
- marker processing is bounded, rejects binary/symlink/non-regular input, and maps most input to typed categories;
- report counts and `BridgeFact`/reason descriptions now match the code;
- the two targeted Gate E packages pass.

However, the new code still does not fully satisfy its own safety contract.

## Remaining findings

### P1 — digest mismatch does not actually keep DeviceProxy disabled

Location: `internal/singbox/router/service_lifecycle.go:622-640`.

When an owned parking record exists and the applied digest differs, the implementation logs that it is “retaining disabled state” and returns. It never checks `dpState.Enabled` and never calls:

```go
SetEnabledSilent(orchestrator.SlotDeviceProxy, false)
```

Therefore this sequence is unsafe:

1. AWG Manager parks DeviceProxy and records ownership.
2. Another actor edits the applied configuration.
3. The same actor or a partial recovery enables DeviceProxy while Mihomo remains primary.
4. Reconcile sees a digest mismatch and returns successfully, leaving DeviceProxy enabled.

This directly contradicts the required behavior “digest mismatch means keep it disabled, retain the record”. It can also reintroduce a listener conflict while Mihomo is running.

Required implementation:

- For any valid owned record, DeviceProxy must be disabled while Mihomo is primary, regardless of whether the digest still matches.
- On mismatch/missing bytes: retain the original record, call `SetEnabledSilent(..., false)` if the slot is enabled, propagate toggle errors, and do not rewrite the record.
- Keep the exact-digest branch as the crash-convergence path, but share the invariant that the parked slot cannot remain enabled.

Required regression test:

1. Park a conflicting enabled slot.
2. Replace its applied bytes with a different valid configuration.
3. Explicitly enable DeviceProxy again.
4. Reconcile while Mihomo remains primary.
5. Assert DeviceProxy is disabled, the original record is byte-for-byte retained, and no debounced reload is scheduled.

The submitted `TestGateE_UserEditsParkedConfigToNonConflictingPortRetainsDisabled` does not expose this bug because it never re-enables the slot before the second reconcile.

### P1 — `connection_failed` marker branch still exports arbitrary marker content

Location: `internal/mihomo/coordinator.go:4926-4930`.

The bounded marker parser is mostly allowlisted, but this special case takes every byte after `failed connecting to ` and exports it:

```go
rawTarget := strings.TrimSpace(trimmed[len("failed connecting to "):])
return "connection_failed: " + RedactSecrets(rawTarget), modTime, true
```

`RedactSecrets` is pattern-based filtering, not an allowlist. An unrecognized credential, private hostname/path, query key, or arbitrary secret can therefore pass through unchanged. Example:

```text
failed connecting to opaque-private-target-MAGICSECRET123
```

becomes public evidence containing `MAGICSECRET123`.

This violates both the plan and the report’s claim that arbitrary marker bytes are never echoed. It also creates an exception to the seeded-secret test, because that test uses an unrecognized prefix and therefore reaches the generic safe category instead of this branch.

Required implementation:

- Return only `connection_failed`, or parse the target into an explicit typed projection with a strict allowlist. The simplest and safest Gate E implementation is the category only.
- Do not append `RedactSecrets(rawTarget)`.

Required regression tests:

- marker `failed connecting to opaque-private-target-MAGIC_UNKNOWN_SECRET_123` serializes without either the target or secret;
- marker containing a URL with path, query, userinfo, fragment, and an unknown credential key serializes only the allowed category;
- behavior remains deterministic and idempotent.

## Verification performed

The new patch matches the current applied working-tree changes:

```text
git apply --check --reverse reports/mihomo/GATE_E_DIFF_2026-09-22.patch
PASS
```

Independent targeted tests:

```bash
go test -count=1 ./internal/singbox/router -run 'TestGateE_'
go test -count=1 ./internal/mihomo -run 'TestGateE_'
```

Result:

```text
ok github.com/hoaxisr/awg-manager/internal/singbox/router 0.145s
ok github.com/hoaxisr/awg-manager/internal/mihomo 1.109s
```

There are now exactly 15 `TestGateE_*` functions in each file, so the revised report’s test-function counts are accurate. Passing existing tests does not cover the two missing adversarial cases above.

## Minimal closure instructions

This is not a request for another redesign or planning cycle. Implement only these two changes and their regression tests:

1. Enforce disabled state on the owned-record digest-mismatch path.
2. Stop exporting the raw target from the `connection_failed` marker category.
3. Run targeted Gate E tests and scoped `git diff --check`.
4. If green, run the previously specified race, broad backend, and frontend validations.
5. Regenerate the patch and correct the resolution report only where behavior/test output changed.

No IPK build or router deployment is required for this closure pass.
