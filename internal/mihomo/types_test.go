package mihomo

import (
	"testing"
)

func TestManifestState_IsValidNext(t *testing.T) {
	tests := []struct {
		current ManifestState
		next    ManifestState
		valid   bool
	}{
		{StateIdle, StateSnapshotSecured, true},
		{StateIdle, StateCommitted, false},

		{StateCommitIntent, StateCommitted, true},
		{StateCommitIntent, StateRecoveryRequired, true},
		{StateCommitIntent, StateRollbackInProgress, false}, // Regression check for commit boundary

		{StateRecoveryRequired, StateRollbackInProgress, true},
		{StateRecoveryRequired, StateAbortInProgress, true},
		{StateRecoveryRequired, StateIdle, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.current)+"_to_"+string(tt.next), func(t *testing.T) {
			if got := tt.current.IsValidNext(tt.next); got != tt.valid {
				t.Errorf("expected transition %s -> %s to be %v, got %v", tt.current, tt.next, tt.valid, got)
			}
		})
	}
}
