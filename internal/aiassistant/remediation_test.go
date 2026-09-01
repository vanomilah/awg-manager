package aiassistant

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type fakeActionExecutor struct {
	calls  atomic.Int32
	action string
	target string
}

func (f *fakeActionExecutor) Apply(_ context.Context, action, target string) error {
	f.calls.Add(1)
	f.action = action
	f.target = target
	return nil
}

func (f *fakeActionExecutor) Verify(_ context.Context, action, target string) (*ActionVerification, error) {
	return &ActionVerification{Status: "passed", Summary: "all good"}, nil
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
