package api

import (
	"context"
	"errors"
)

var (
	ErrRecoveryRequired = errors.New("RECOVERY_REQUIRED")
)

// NativeMutationApplier coordinates transactional mutations against the Mihomo engine.
type NativeMutationApplier interface {
	ApplyNativeMutation(ctx context.Context, mutateFn func() error) error
	ApplyDraftOnly(ctx context.Context, mutateFn func() error) error
	ApplyPendingDraft(ctx context.Context) error
	IsDegraded() bool
	Reconcile(ctx context.Context, action string, force bool) error
}
