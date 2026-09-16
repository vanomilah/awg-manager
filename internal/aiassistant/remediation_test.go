package aiassistant

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeActionExecutor struct {
	calls        atomic.Int32
	action       string
	target       string
	verification *ActionVerification
}

func (f *fakeActionExecutor) Apply(_ context.Context, action, target string) error {
	f.calls.Add(1)
	f.action = action
	f.target = target
	return nil
}

func (f *fakeActionExecutor) Verify(_ context.Context, action, target string) (*ActionVerification, error) {
	if f.verification != nil {
		return f.verification, nil
	}
	return &ActionVerification{Status: "passed", Summary: "all good"}, nil
}

func TestApplyActionDoesNotClaimSuccessWhenVerificationFails(t *testing.T) {
	svc := NewService(nil)
	exec := &fakeActionExecutor{verification: &ActionVerification{Status: "failed", Summary: "Mihomo is still stopped"}}
	svc.SetActions(exec)
	proposal := validatedRemediationProposal("mihomo.restart", "")
	svc.state.Proposal = proposal

	if err := svc.ApplyAction(proposal.ID); err != nil {
		t.Fatal(err)
	}
	state := svc.Status()
	if state.Proposal == nil || state.Proposal.Status != "verification_failed" {
		t.Fatalf("proposal = %+v", state.Proposal)
	}
	if len(state.Messages) == 0 || !strings.Contains(state.Messages[len(state.Messages)-1].Content, "проблема не устранена") {
		t.Fatalf("messages = %+v", state.Messages)
	}
}

type transactionalActionExecutor struct {
	fakeActionExecutor
	rolledBack bool
}

func (f *transactionalActionExecutor) Snapshot(context.Context, string, string) (*ActionSnapshot, error) {
	return &ActionSnapshot{Action: "routing.switch_engine", Value: "sing-box"}, nil
}

func (f *transactionalActionExecutor) Rollback(_ context.Context, snapshot ActionSnapshot) error {
	f.rolledBack = snapshot.Value == "sing-box"
	return nil
}

func TestApplyActionRollsBackFailedTransactionalVerification(t *testing.T) {
	svc := NewService(nil)
	exec := &transactionalActionExecutor{fakeActionExecutor: fakeActionExecutor{
		verification: &ActionVerification{Status: "failed", Summary: "Mihomo did not start"},
	}}
	svc.SetActions(exec)
	proposal := validatedRemediationProposal("routing.switch_engine", "mihomo")
	if proposal == nil {
		t.Fatal("proposal is nil")
	}
	svc.state.Proposal = proposal

	if err := svc.ApplyAction(proposal.ID); err != nil {
		t.Fatal(err)
	}
	state := svc.Status()
	if !exec.rolledBack || state.Proposal == nil || state.Proposal.Status != "rolled_back" || !state.Proposal.RolledBack {
		t.Fatalf("proposal=%+v rolledBack=%v", state.Proposal, exec.rolledBack)
	}
}

func TestSwitchEngineProposalAcceptsOnlyKnownEngines(t *testing.T) {
	if validatedRemediationProposal("routing.switch_engine", "mihomo") == nil {
		t.Fatal("mihomo target rejected")
	}
	if validatedRemediationProposal("routing.switch_engine", "sing-box") == nil {
		t.Fatal("sing-box target rejected")
	}
	if validatedRemediationProposal("routing.switch_engine", "other") != nil {
		t.Fatal("unknown engine accepted")
	}
}

func TestRoutingModeTransactionSnapshotsAndRestores(t *testing.T) {
	current := "tproxy"
	registry := NewActionRegistry(ActionHandlers{
		CurrentRoutingMode: func(context.Context) (string, error) { return current, nil },
		SwitchRoutingMode: func(_ context.Context, mode string) error {
			current = mode
			return nil
		},
	})
	snapshot, err := registry.Snapshot(context.Background(), "routing.switch_mode", "fakeip-tun")
	if err != nil || snapshot == nil || snapshot.Value != "tproxy" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if err := registry.Apply(context.Background(), "routing.switch_mode", "fakeip-tun"); err != nil {
		t.Fatal(err)
	}
	if current != "fakeip-tun" {
		t.Fatalf("current=%q", current)
	}
	if err := registry.Rollback(context.Background(), *snapshot); err != nil {
		t.Fatal(err)
	}
	if current != "tproxy" {
		t.Fatalf("rollback current=%q", current)
	}
}

func TestSwitchModeProposalAcceptsOnlyKnownModes(t *testing.T) {
	for _, mode := range []string{"off", "tproxy", "fakeip-tun", "policy-tun"} {
		if validatedRemediationProposal("routing.switch_mode", mode) == nil {
			t.Fatalf("mode %q rejected", mode)
		}
	}
	if validatedRemediationProposal("routing.switch_mode", "shell") != nil {
		t.Fatal("unknown mode accepted")
	}
}

func TestSystemActionTargetsAreStrictlyValidated(t *testing.T) {
	for _, tc := range []struct{ action, target string }{
		{"service.restart", "S90demo"},
		{"service.start", "K50worker"},
		{"opkg.install", "curl"},
		{"opkg.upgrade", "ca-bundle"},
		{"opkg.remove", "demo_pkg"},
		{"opkg.update", ""},
	} {
		if validatedRemediationProposal(tc.action, tc.target) == nil {
			t.Fatalf("valid proposal rejected: %+v", tc)
		}
	}
	for _, tc := range []struct{ action, target string }{
		{"service.restart", "../../S90demo"},
		{"service.stop", "dropbear"},
		{"opkg.install", "curl; reboot"},
		{"opkg.update", "unexpected"},
	} {
		if validatedRemediationProposal(tc.action, tc.target) != nil {
			t.Fatalf("unsafe proposal accepted: %+v", tc)
		}
	}
}

func TestSystemMutationsAreNeverAutoFixed(t *testing.T) {
	for _, action := range []string{"service.start", "service.stop", "service.restart", "opkg.update", "opkg.install", "opkg.upgrade", "opkg.remove", "routing.switch_engine", "routing.switch_mode"} {
		if autoFixActionAllowed(action) {
			t.Fatalf("unsafe auto-fix action allowed: %s", action)
		}
	}
	if !autoFixActionAllowed("mihomo.reload") {
		t.Fatal("known reversible low-risk action should remain eligible for auto-fix")
	}
}

func TestRemediationForSingboxProxyPortRequiresConfirmation(t *testing.T) {
	proposal := remediationForFindings([]Finding{{Source: "singbox_proxy_port"}}, "")
	if proposal == nil || proposal.Action != "singbox.restart" || proposal.Status != "pending" || proposal.ID == "" {
		t.Fatalf("proposal = %+v", proposal)
	}
	exec := &fakeActionExecutor{}
	svc := NewService(nil)
	svc.SetActions(exec)
	svc.state.Proposal = proposal
	if err := svc.ApplyAction(proposal.ID); err != nil {
		t.Fatal(err)
	}
	if exec.calls.Load() != 1 || svc.Status().Proposal.Status != "applied" {
		t.Fatalf("state = %+v calls = %d", svc.Status().Proposal, exec.calls.Load())
	}
	if svc.Status().ReadOnly {
		t.Fatal("state must record that a confirmed mutation was attempted")
	}
	if svc.Status().Proposal.Verification == nil || svc.Status().Proposal.Verification.Status != "passed" {
		t.Fatalf("verification = %+v", svc.Status().Proposal.Verification)
	}
	if err := svc.ApplyAction(proposal.ID); err == nil {
		t.Fatal("proposal was applied twice")
	}
}

func TestRemediationFromModelAction(t *testing.T) {
	answer := "Я обнаружил, что туннель nwg0 потерял соединение.\nACTION: {\"action\": \"tunnel.restart\", \"target\": \"nwg0\", \"title\": \"Перезапустить nwg0\", \"description\": \"Перезапуск туннеля\", \"risk\": \"low\"}"
	proposal := remediationForFindings(nil, answer)
	if proposal == nil || proposal.Action != "tunnel.restart" || proposal.Target != "nwg0" {
		t.Fatalf("proposal = %+v", proposal)
	}
}

func TestRemediationFromModelUsesServerMetadata(t *testing.T) {
	answer := `ACTION: {"action":"mihomo.reload","title":"Безопасная проверка","description":"ничего не изменится","risk":"none"}`
	proposal := remediationForFindings(nil, answer)
	if proposal == nil {
		t.Fatal("proposal is nil")
	}
	if proposal.Title != "Перезагрузить конфигурацию Mihomo" || proposal.Risk != "low" {
		t.Fatalf("model metadata escaped allowlist: %+v", proposal)
	}
}

func TestRemediationFromModelRejectsUnknownOrUnsafeAction(t *testing.T) {
	for _, answer := range []string{
		`ACTION: {"action":"shell.run","target":"reboot"}`,
		`ACTION: {"action":"tunnel.restart","target":"nwg0; reboot"}`,
		`ACTION: {"action":"tunnel.restart"}`,
		`ACTION: {"action":"singbox.restart","target":"nwg0"}`,
	} {
		if proposal := remediationForFindings(nil, answer); proposal != nil {
			t.Fatalf("unsafe proposal accepted: %+v", proposal)
		}
	}
}

func TestRemediationFromStructuredToolStep(t *testing.T) {
	proposal := remediationFromToolSteps([]ToolStep{{
		Name: "remediation.propose", Status: "passed",
		Evidence: []string{`{"action":"subscription.update","target":"sub-123","risk":"none","title":"fake"}`},
	}})
	if proposal == nil || proposal.Action != "subscription.update" || proposal.Target != "sub-123" || proposal.Risk != "low" {
		t.Fatalf("proposal = %+v", proposal)
	}
}

func TestAutoFixAppliesAutomatically(t *testing.T) {
	dir := t.TempDir()
	store, err := NewConfigStore(dir + "/ai.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(ConfigUpdate{Enabled: true, AutoFix: true, Provider: "openai", Model: "test", APIKey: "secret"}); err != nil {
		t.Fatal(err)
	}

	exec := &fakeActionExecutor{}
	svc := NewService(nil)
	svc.SetModel(store, &fakeModel{})
	svc.SetActions(exec)

	// Simulate completeDiagnosis with auto-fixable finding
	svc.completeDiagnosis(Stats{}, []Finding{{Source: "tunnel_handshake_stale", Detail: "tunnel-test"}}, "Модель советует перезапуск", nil, "openai")

	if exec.calls.Load() != 1 || exec.action != "tunnel.restart" || exec.target != "tunnel-test" {
		t.Fatalf("exec = %+v calls=%d", exec, exec.calls.Load())
	}
	state := svc.Status()
	if state.Proposal == nil || state.Proposal.Status != "applied" || !state.Proposal.AutoApplied {
		t.Fatalf("state.Proposal = %+v", state.Proposal)
	}
}

func TestExpiredRemediationIsRejected(t *testing.T) {
	svc := NewService(nil)
	svc.SetActions(&fakeActionExecutor{})
	svc.state.Proposal = &RemediationProposal{ID: "old", Action: "singbox.restart", Status: "pending", CreatedAt: time.Now().Add(-11 * time.Minute)}
	if err := svc.ApplyAction("old"); err == nil || svc.Status().Proposal.Status != "expired" {
		t.Fatalf("error = %v proposal = %+v", err, svc.Status().Proposal)
	}
}
