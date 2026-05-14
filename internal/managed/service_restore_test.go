package managed

import (
	"context"
	"strings"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/ndms/query"
	"github.com/hoaxisr/awg-manager/internal/storage"
)

func TestRestore_CreatesNewServerHappyPath(t *testing.T) {
	dir := t.TempDir()
	store := storage.NewSettingsStore(dir)
	_, _ = store.Load()

	// stateAwareGetter answers /show/interface/ from the live SettingsStore
	// so preflight's listUsedSubnets does not panic on nil queries.
	getter := &stateAwareGetter{store: store}
	ifaces := query.NewInterfaceStoreWithTTL(getter, query.NopLogger(), 0, 0)
	queries := &query.Queries{
		Interfaces: ifaces,
		WGServers:  query.NewWGServerStore(getter, query.NopLogger(), ifaces),
	}
	poster := &fakePoster{}
	s := &Service{settings: store, transport: poster, queries: queries}

	outcomes := s.Restore(context.Background(), []ManagedServerExport{{
		InterfaceName: "Wireguard0",
		Description:   "Home VPN",
		Address:       "10.99.0.1",
		Mask:          "255.255.255.0",
		ListenPort:    51900,
		PrivateKey:    "PRIVKEY=",
		NATEnabled:    true,
		Peers:         []storage.ManagedPeer{},
	}}, RestoreOptions{})

	if len(outcomes) != 1 || outcomes[0].Action != "created" {
		t.Fatalf("outcomes: %+v", outcomes)
	}
	// create + configure + set-key + set-NAT = at least 4 RCI calls.
	if len(poster.posts) < 4 {
		t.Errorf("expected at least 4 RCI calls (create, configure, set-key, set-NAT); got %d", len(poster.posts))
	}
	got, ok := store.GetManagedServerByID("Wireguard0")
	if !ok {
		t.Fatalf("server not persisted to storage")
	}
	if got.PrivateKey != "PRIVKEY=" {
		t.Errorf("PrivateKey not persisted: got %q", got.PrivateKey)
	}
}

func TestRestore_RejectsEmptyPrivateKey(t *testing.T) {
	dir := t.TempDir()
	store := storage.NewSettingsStore(dir)
	_, _ = store.Load()

	s := &Service{settings: store}
	outcomes := s.Restore(context.Background(), []ManagedServerExport{{
		InterfaceName: "Wireguard0",
		Address:       "10.0.0.1",
		Mask:          "255.255.255.0",
		ListenPort:    51820,
		// PrivateKey deliberately empty
	}}, RestoreOptions{})

	if len(outcomes) != 1 || outcomes[0].Action != "failed" {
		t.Fatalf("outcomes: %+v", outcomes)
	}
}

func TestRestore_PreflightDetectsInvalidAddress(t *testing.T) {
	dir := t.TempDir()
	store := storage.NewSettingsStore(dir)
	_, _ = store.Load()

	s := &Service{settings: store}
	outcomes := s.Restore(context.Background(), []ManagedServerExport{{
		InterfaceName: "Wireguard0",
		Address:       "not-an-ip",
		Mask:          "255.255.255.0",
		ListenPort:    51820,
		PrivateKey:    "k0",
	}}, RestoreOptions{})

	if outcomes[0].Action != "conflict" {
		t.Fatalf("action: %q, conflicts: %v", outcomes[0].Action, outcomes[0].Conflicts)
	}
	found := false
	for _, c := range outcomes[0].Conflicts {
		if strings.Contains(c, "not a valid IP") {
			found = true
		}
	}
	if !found {
		t.Errorf("conflicts: %v (expected invalid-IP reason)", outcomes[0].Conflicts)
	}
}
