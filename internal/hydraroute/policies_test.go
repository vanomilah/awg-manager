package hydraroute

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/internal/ndms/command"
	"github.com/hoaxisr/awg-manager/internal/ndms/query"
)

// fakePoster records Post payloads for assertion and returns a benign response.
type fakePoster struct {
	mu       sync.Mutex
	payloads []any
}

func (f *fakePoster) Post(_ context.Context, payload any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payloads = append(f.payloads, payload)
	return json.RawMessage(`{}`), nil
}

func (f *fakePoster) Payloads() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]any, len(f.payloads))
	copy(out, f.payloads)
	return out
}

// nopPublisher satisfies command.StatusPublisher without side effects.
type nopPublisher struct{}

func (nopPublisher) Publish(string, any) {}

// newTestQueries builds a query.Queries backed by a controllable FakeGetter.
func newTestQueries() (*query.Queries, *query.FakeGetter) {
	g := query.NewFakeGetter()
	q := query.NewQueries(query.Deps{
		Getter: g,
		Logger: query.NopLogger(),
		IsOS5:  func() bool { return true },
	})
	return q, g
}

// newTestPolicyCommands builds a real *command.PolicyCommands wired to a fakePoster.
func newTestPolicyCommands(q *query.Queries) (*command.PolicyCommands, *fakePoster) {
	poster := &fakePoster{}
	sc := command.NewSaveCoordinator(poster, nopPublisher{}, 500*time.Millisecond, 5*time.Second, 0, nil)
	return command.NewPolicyCommands(poster, sc, q, nil), poster
}

func TestListPolicyNames_ParsesKeys(t *testing.T) {
	q, g := newTestQueries()
	g.SetJSON("/show/rc/ip/policy", `{
		"Policy0": {"description": "Mallware"},
		"HydraRoute": {"description": ""}
	}`)
	svc := &Service{queries: q}

	got, err := svc.ListPolicyNames(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sort.Strings(got)
	want := []string{"HydraRoute", "Policy0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if g.Calls("/show/rc/ip/policy") != 1 {
		t.Errorf("expected 1 RCI fetch, got %d", g.Calls("/show/rc/ip/policy"))
	}
}

func TestListPolicyNames_NoNDMS(t *testing.T) {
	svc := &Service{}
	got, err := svc.ListPolicyNames(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestListPolicyNames_NDMSError(t *testing.T) {
	q, g := newTestQueries()
	g.SetError("/show/rc/ip/policy", errors.New("boom"))
	svc := &Service{queries: q}

	_, err := svc.ListPolicyNames(context.Background())
	if err == nil {
		t.Fatal("expected error propagation")
	}
}

func TestEnsurePolicyInterfaces_OrderIsZeroBased(t *testing.T) {
	// Regression: Keenetic rejects 'ip policy permit order N' when N is
	// out of range. The first permit on a fresh policy MUST be order=0;
	// previously we sent order=1 and got "invalid order: 1".
	q, _ := newTestQueries()
	cmds, poster := newTestPolicyCommands(q)
	svc := &Service{policies: cmds}

	err := svc.EnsurePolicyInterfaces(
		context.Background(),
		"NewPolicy",
		[]string{"PPPoE0", "Wireguard0", "Wireguard1"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payloads := poster.Payloads()
	if len(payloads) != 3 {
		t.Fatalf("expected 3 Post calls, got %d", len(payloads))
	}

	wantOrders := []int{0, 1, 2}
	wantIfaces := []string{"PPPoE0", "Wireguard0", "Wireguard1"}
	for i, payload := range payloads {
		permit := digPermit(t, payload, "NewPolicy")
		gotOrder, ok := permit["order"].(int)
		if !ok {
			t.Fatalf("call %d: permit.order missing/wrong type: %v", i, permit["order"])
		}
		if gotOrder != wantOrders[i] {
			t.Errorf("call %d: order = %d, want %d", i, gotOrder, wantOrders[i])
		}
		if iface, _ := permit["interface"].(string); iface != wantIfaces[i] {
			t.Errorf("call %d: interface = %q, want %q", i, iface, wantIfaces[i])
		}
	}
}

// digPermit drills into the nested RCI payload to fetch the permit object.
func digPermit(t *testing.T, payload any, policyName string) map[string]any {
	t.Helper()
	root, ok := payload.(map[string]any)
	if !ok {
		t.Fatalf("payload not a map: %T", payload)
	}
	ip, _ := root["ip"].(map[string]any)
	policy, _ := ip["policy"].(map[string]any)
	named, _ := policy[policyName].(map[string]any)
	permit, _ := named["permit"].(map[string]any)
	if permit == nil {
		t.Fatalf("permit object missing from payload: %+v", root)
	}
	return permit
}

func TestCleanGhostPolicies(t *testing.T) {
	svc, domainPath, _ := setupRuleFiles(t)

	// Set up domain.conf targeting empty user policies (EmptyUser, Streaming, Work)
	// and interface names (awgm0). Previously, EmptyUser would have been deleted
	// simply because it was in domain.conf hrTargets.
	domainContent := `address=/test1.com/EmptyUser
address=/test2.com/Streaming
address=/test3.com/Work
address=/test4.com/awgm0
`
	if err := os.WriteFile(domainPath, []byte(domainContent), 0644); err != nil {
		t.Fatalf("failed to write domain.conf: %v", err)
	}

	q, g := newTestQueries()
	g.SetJSON("/show/rc/ip/policy", `{
		"Policy0": {"description": "System Mallware"},
		"Streaming": {"description": "User Policy"},
		"Work": {"description": "", "permit": [{"interface": "PPPoE0", "enabled": true}]},
		"EmptyUser": {"description": ""},
		"awgm0": {"description": ""},
		"Wireguard2": {"description": ""},
		"opkgtun10": {"description": ""},
		"sstp0": {"description": ""},
		"openvpn1": {"description": ""}
	}`)

	cmds, poster := newTestPolicyCommands(q)
	svc.queries = q
	svc.policies = cmds

	deleted, err := svc.CleanGhostPolicies(context.Background())
	if err != nil {
		t.Fatalf("CleanGhostPolicies failed: %v", err)
	}

	if deleted != 5 {
		t.Errorf("CleanGhostPolicies deleted %d, want 5 (awgm0, Wireguard2, opkgtun10, sstp0, openvpn1)", deleted)
	}

	payloads := poster.Payloads()
	if len(payloads) != 5 {
		t.Fatalf("expected 5 DeletePolicy calls, got %d", len(payloads))
	}

	deletedNames := make([]string, 0, len(payloads))
	for _, payload := range payloads {
		root, ok := payload.(map[string]any)
		if !ok {
			continue
		}
		ip, _ := root["ip"].(map[string]any)
		policy, _ := ip["policy"].(map[string]any)
		for name := range policy {
			deletedNames = append(deletedNames, name)
		}
	}
	sort.Strings(deletedNames)
	wantDeleted := []string{"Wireguard2", "awgm0", "openvpn1", "opkgtun10", "sstp0"}
	if !reflect.DeepEqual(deletedNames, wantDeleted) {
		t.Errorf("deleted policies = %v, want %v", deletedNames, wantDeleted)
	}
}

func TestCleanGhostPolicies_Concurrent(t *testing.T) {
	q, g := newTestQueries()
	g.SetJSON("/show/rc/ip/policy", `{
		"awgm0": {"description": ""},
		"Wireguard2": {"description": ""}
	}`)

	cmds, _ := newTestPolicyCommands(q)
	svc := &Service{
		queries:  q,
		policies: cmds,
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.CleanGhostPolicies(context.Background())
		}()
	}
	wg.Wait()
}

func TestIsGhostPolicyName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		// Keenetic system policies: never ghosts (case-insensitive)
		{"Policy0", false},
		{"Policy1", false},
		{"Policy10", false},
		{"policy0", false},
		{"policy10", false},
		{"POLICY0", false},

		// Legitimate user policy names
		{"EmptyUser", false},
		{"Streaming", false},
		{"Work", false},
		{"Direct", false},
		{"Gaming", false},
		{"PolicyUser", false},

		// Prefixed targets
		{"wan:ppp0", true},
		{"wan:PPPoE0", true},
		{"system:Wireguard0", true},

		// Interface naming patterns matching ghostPolicyPrefixRE
		{"awgm0", true},
		{"awg1", true},
		{"opkgtun10", true},
		{"nwg0", true},
		{"wireguard2", true},
		{"ppp0", true},
		{"pppoe1", true},
		{"eth0", true},
		{"usb0", true},
		{"wlan0", true},
		{"wifi1", true},
		{"br0", true},
		{"tun0", true},
		{"tap0", true},
		{"sstp0", true},
		{"openvpn1", true},
		{"ovpn0", true},
		{"l2tp0", true},
		{"ipsec1", true},
		{"gre0", true},
		{"eoip2", true},

		// Case-insensitive check
		{"Wireguard0", true},
		{"OPKGTUN1", true},
		{"AWGM3", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGhostPolicyName(tt.name)
			if got != tt.want {
				t.Errorf("isGhostPolicyName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}
