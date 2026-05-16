package router

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeExec struct {
	calls    []fakeCall
	err      error
	runIPErr error
}

type fakeCall struct {
	kind  string
	args  []string
	stdin string
}

// errENOENT mimics the kernel's "rule not found" exit so the drain
// loops terminate after a single pass — without this, fakeExec.runIP
// returning nil for `ip rule del` causes the cap-bounded drain loop
// to record N entries (or, before the cap, to OOM the test process).
var errENOENT = errIPRule("RTNETLINK answers: No such file or directory")

type errIPRule string

func (e errIPRule) Error() string { return string(e) }

func (f *fakeExec) restoreNoflush(_ context.Context, input string) error {
	f.calls = append(f.calls, fakeCall{kind: "restore", stdin: input})
	return f.err
}

func (f *fakeExec) runIPTables(_ context.Context, args ...string) error {
	f.calls = append(f.calls, fakeCall{kind: "iptables", args: args})
	return f.err
}

func (f *fakeExec) runIP(_ context.Context, args ...string) error {
	f.calls = append(f.calls, fakeCall{kind: "ip", args: args})
	if f.runIPErr != nil {
		return f.runIPErr
	}
	if f.err != nil {
		return f.err
	}
	// Make `ip rule del fwmark ...` return ENOENT after the first call
	// so drain loops don't append forever.
	if len(args) >= 4 && args[0] == "rule" && args[1] == "del" {
		return errENOENT
	}
	return nil
}

func newFakeIPTables(fe *fakeExec) *IPTables {
	return &IPTables{
		restoreNoflush: fe.restoreNoflush,
		runIPTables:    fe.runIPTables,
		runIP:          fe.runIP,
	}
}

func newFakeExec() *fakeExec {
	return &fakeExec{}
}

func TestBuildTProxyModulePath(t *testing.T) {
	got := buildTProxyModulePath("5.15.0-mips")
	want := "/lib/modules/5.15.0-mips/xt_TPROXY.ko"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestKernelModuleName(t *testing.T) {
	if kernelModuleName() != "xt_TPROXY" {
		t.Errorf("got %q", kernelModuleName())
	}
}

func TestBuildRestoreInput_PolicyMark_EmitsBothJumps(t *testing.T) {
	spec := RestoreInputSpec{PolicyMark: "0xffffaaa"}
	out := buildRestoreInput(spec)
	// `-A PREROUTING` (append, not -I 1): we want our jump AFTER NDMS
	// _NDM_* chains so the policy connmark is set before we evaluate.
	// `-g` (goto, not -j): bypass ACCEPTs terminate mangle table cleanly
	// instead of unwinding back to PREROUTING and re-entering NDMS rules.
	// No connmark/ctdir filters on the jump itself — those moved into
	// the chain body.
	wantMangle := "-A PREROUTING -p udp -m conntrack ! --ctstate INVALID -g " + ChainName
	if !strings.Contains(out, wantMangle) {
		t.Errorf("missing mangle PREROUTING jump\nwant: %s\ngot:\n%s", wantMangle, out)
	}
	wantNat := "-A PREROUTING -p tcp -m conntrack ! --ctstate INVALID -g " + RedirectChain
	if !strings.Contains(out, wantNat) {
		t.Errorf("missing nat PREROUTING jump\nwant: %s\ngot:\n%s", wantNat, out)
	}
	// Legacy `-I PREROUTING 1 -j AWGM-TPROXY/AWGM-REDIRECT` syntax must
	// not appear — it caused the post-NAT-source bug fixed on 2026-05-16.
	// (AWGM-DNS-OFFLOAD does legitimately use `-I PREROUTING 1` to fire
	// before NDMS DNAT chains — see DNSOffloadChain const docs.)
	for _, bad := range []string{
		"-I PREROUTING 1 -p udp -m connmark --mark",
		"-I PREROUTING 1 -p tcp -m connmark --mark",
		"-I PREROUTING 1 -j " + ChainName,
		"-I PREROUTING 1 -j " + RedirectChain,
	} {
		if strings.Contains(out, bad) {
			t.Errorf("legacy %q syntax must not appear:\n%s", bad, out)
		}
	}
	if strings.Contains(out, "-j "+ChainName) || strings.Contains(out, "-j "+RedirectChain) {
		t.Errorf("PREROUTING entries to TPROXY/REDIRECT must use -g, not -j:\n%s", out)
	}
}

func TestBuildRestoreInput_EmptyMark_NoPrerouting(t *testing.T) {
	spec := RestoreInputSpec{PolicyMark: ""}
	out := buildRestoreInput(spec)
	if strings.Contains(out, "-A PREROUTING") || strings.Contains(out, "-I PREROUTING") {
		t.Errorf("expected no PREROUTING entry for empty mark, got:\n%s", out)
	}
}

func TestBuildRestoreInput_DNSOffloadChain(t *testing.T) {
	spec := RestoreInputSpec{PolicyMark: "0xffffaaa"}
	out := buildRestoreInput(spec)

	// AWGM-DNS-OFFLOAD: DNATs non-policy UDP/TCP DNS aimed at a router
	// LOCAL IP to 127.0.0.1:53. Two filters protect policy traffic:
	//   1. dst must be addrtype LOCAL (excludes external DNS like 8.8.8.8)
	//   2. connmark must NOT match the policy mark (excludes in-policy)
	// Jump goes to PREROUTING position 1 so it fires before NDMS DNAT
	// chains, beating any DNS rewrite NDMS might be doing.
	wantChain := ":AWGM-DNS-OFFLOAD - [0:0]"
	if !strings.Contains(out, wantChain) {
		t.Errorf("missing AWGM-DNS-OFFLOAD chain declaration\nin:\n%s", out)
	}
	for _, proto := range []string{"udp", "tcp"} {
		wantRule := "-A AWGM-DNS-OFFLOAD -i br+ -p " + proto +
			" --dport 53 -m addrtype --dst-type LOCAL -m connmark ! --mark 0xffffaaa" +
			" -j DNAT --to-destination 127.0.0.1:53"
		if !strings.Contains(out, wantRule) {
			t.Errorf("missing %s DNAT rule\nwant: %s\nin:\n%s", proto, wantRule, out)
		}
	}
	wantJump := "-I PREROUTING 1 -j AWGM-DNS-OFFLOAD"
	if !strings.Contains(out, wantJump) {
		t.Errorf("missing PREROUTING jump to AWGM-DNS-OFFLOAD\nin:\n%s", out)
	}
}

func TestBuildRestoreInput_DNSOffload_EmptyMark_OnlyChainHeader(t *testing.T) {
	spec := RestoreInputSpec{PolicyMark: ""}
	out := buildRestoreInput(spec)

	// With empty policy mark we still emit the chain header so a
	// subsequent re-Install with a mark recreates rules cleanly, but
	// no DNAT rules and no PREROUTING jump.
	if !strings.Contains(out, ":AWGM-DNS-OFFLOAD - [0:0]") {
		t.Errorf("expected chain header even when policy mark empty:\n%s", out)
	}
	if strings.Contains(out, "AWGM-DNS-OFFLOAD -i br+") {
		t.Errorf("expected no DNAT rules when policy mark empty:\n%s", out)
	}
	if strings.Contains(out, "-I PREROUTING 1 -j AWGM-DNS-OFFLOAD") {
		t.Errorf("expected no PREROUTING jump when policy mark empty:\n%s", out)
	}
}

func TestBuildRestoreInput_PolicyFilterInsideChain(t *testing.T) {
	spec := RestoreInputSpec{PolicyMark: "0xffffaaa"}
	out := buildRestoreInput(spec)

	// Policy filter MUST be the first ACCEPT inside each chain — emits
	// only when PolicyMark is set, exits the table for non-policy traffic.
	wantMangleFilter := "-A AWGM-TPROXY -m connmark ! --mark 0xffffaaa -j ACCEPT"
	if !strings.Contains(out, wantMangleFilter) {
		t.Errorf("missing in-chain policy filter\nwant: %s\ngot:\n%s", wantMangleFilter, out)
	}
	wantNatFilter := "-A AWGM-REDIRECT -m connmark ! --mark 0xffffaaa -j ACCEPT"
	if !strings.Contains(out, wantNatFilter) {
		t.Errorf("missing in-chain policy filter\nwant: %s\ngot:\n%s", wantNatFilter, out)
	}

	// Reply-direction filter follows the policy filter — NDMS's connmark
	// applies to both directions of the conntrack entry, so returns from
	// the internet would also match the policy filter above; we ACCEPT
	// them out so they don't re-enter TPROXY.
	wantMangleReply := "-A AWGM-TPROXY -m conntrack --ctdir REPLY -j ACCEPT"
	if !strings.Contains(out, wantMangleReply) {
		t.Errorf("missing in-chain reply filter\nwant: %s\ngot:\n%s", wantMangleReply, out)
	}
	wantNatReply := "-A AWGM-REDIRECT -m conntrack --ctdir REPLY -j ACCEPT"
	if !strings.Contains(out, wantNatReply) {
		t.Errorf("missing in-chain reply filter\nwant: %s\ngot:\n%s", wantNatReply, out)
	}

	// Filters MUST precede the catch-all TPROXY/REDIRECT.
	filterIdx := strings.Index(out, wantMangleFilter)
	tproxyIdx := strings.Index(out, "-A AWGM-TPROXY -p udp -j TPROXY")
	if filterIdx < 0 || tproxyIdx < 0 || filterIdx > tproxyIdx {
		t.Errorf("policy filter must precede catch-all TPROXY: filter=%d tproxy=%d", filterIdx, tproxyIdx)
	}
}

func TestBuildRestoreInput_TablesAndRulesPresent(t *testing.T) {
	input := buildRestoreInput(RestoreInputSpec{PolicyMark: "0xffffaaa"})

	expected := []string{
		// mangle table
		"*mangle",
		":AWGM-TPROXY - [0:0]",
		"-A AWGM-TPROXY -d 127.0.0.0/8 -j ACCEPT",
		"-A AWGM-TPROXY -d 192.168.0.0/16 -j ACCEPT",
		"-A AWGM-TPROXY -m mark --mark 0xff -j ACCEPT",
		"-A AWGM-TPROXY -p udp -j TPROXY --on-port 51271 --on-ip 127.0.0.1 --tproxy-mark 0x1",
		// nat table
		"*nat",
		":AWGM-REDIRECT - [0:0]",
		"-A AWGM-REDIRECT -d 127.0.0.0/8 -j ACCEPT",
		"-A AWGM-REDIRECT -d 192.168.0.0/16 -j ACCEPT",
		"-A AWGM-REDIRECT -p tcp --dport 79 -j ACCEPT",
		"-A AWGM-REDIRECT -p tcp -j REDIRECT --to-ports 51272",
		// AWGM-DNS-OFFLOAD chain
		":AWGM-DNS-OFFLOAD - [0:0]",
		"-A AWGM-DNS-OFFLOAD -i br+ -p udp --dport 53 -m addrtype --dst-type LOCAL -m connmark ! --mark 0xffffaaa -j DNAT --to-destination 127.0.0.1:53",
		"-A AWGM-DNS-OFFLOAD -i br+ -p tcp --dport 53 -m addrtype --dst-type LOCAL -m connmark ! --mark 0xffffaaa -j DNAT --to-destination 127.0.0.1:53",
		"-I PREROUTING 1 -j AWGM-DNS-OFFLOAD",
		// both tables commit
		"COMMIT",
	}
	for _, line := range expected {
		if !strings.Contains(input, line) {
			t.Errorf("missing line: %q\nin:\n%s", line, input)
		}
	}
	// TCP TPROXY MUST NOT appear in mangle (we moved TCP to nat REDIRECT).
	if strings.Contains(input, "-A AWGM-TPROXY -p tcp -j TPROXY") {
		t.Errorf("legacy TCP TPROXY rule must not be present:\n%s", input)
	}
	// Legacy `-j RETURN` bypass must not appear — under -g semantics it
	// would unwind back to PREROUTING and let bypass'd packets re-enter
	// NDMS rules. ACCEPT terminates the table cleanly.
	if strings.Contains(input, "-j RETURN") {
		t.Errorf("legacy -j RETURN bypass must not be present:\n%s", input)
	}
}

func TestIPTablesInstallSequence(t *testing.T) {
	fe := &fakeExec{}
	it := newFakeIPTables(fe)
	if err := it.Install(context.Background(), RestoreInputSpec{PolicyMark: "0xffffaaa"}); err != nil {
		t.Fatal(err)
	}
	// Find the operation phases in the call list rather than asserting
	// strict positions: removeSourceHooks runs `iptables -S PREROUTING`
	// across mangle+nat first (cleans stale jumps), then iptables-restore,
	// then `ip rule del` drain, then `ip rule add`, then `ip route add`.
	var (
		restoreSeen   bool
		ruleAddSeen   bool
		ruleAddArgs   string
		routeAddSeen  bool
		ruleDrainSeen bool
	)
	for _, c := range fe.calls {
		switch c.kind {
		case "restore":
			restoreSeen = true
			if !strings.Contains(c.stdin, "AWGM-TPROXY") {
				t.Errorf("restore stdin missing AWGM-TPROXY:\n%s", c.stdin)
			}
			if !strings.Contains(c.stdin, "AWGM-REDIRECT") {
				t.Errorf("restore stdin missing AWGM-REDIRECT:\n%s", c.stdin)
			}
		case "ip":
			args := strings.Join(c.args, " ")
			if strings.Contains(args, "rule del fwmark") {
				ruleDrainSeen = true
			}
			if strings.Contains(args, "rule add fwmark") {
				ruleAddSeen = true
				ruleAddArgs = args
			}
			if strings.Contains(args, "route add local") {
				routeAddSeen = true
			}
		}
	}
	if !restoreSeen {
		t.Errorf("expected iptables-restore call")
	}
	if !ruleDrainSeen {
		t.Errorf("expected ip rule del drain pass")
	}
	if !ruleAddSeen || !strings.Contains(ruleAddArgs, "priority 30000") {
		t.Errorf("expected ip rule add with priority 30000, got %q", ruleAddArgs)
	}
	if !routeAddSeen {
		t.Errorf("expected ip route add local")
	}
}

func TestIPTablesUninstallSequence(t *testing.T) {
	fe := &fakeExec{err: nil}
	it := newFakeIPTables(fe)
	if err := it.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fe.calls) < 3 {
		t.Errorf("expected >=3 calls, got %d", len(fe.calls))
	}
}

func TestWriteNetfilterHookContainsPidofGuard(t *testing.T) {
	tmp := t.TempDir()
	orig := netfilterHookPath
	netfilterHookPath = filepath.Join(tmp, "50-awgm-tproxy.sh")
	t.Cleanup(func() { netfilterHookPath = orig })

	if err := writeNetfilterHook(); err != nil {
		t.Fatalf("writeNetfilterHook: %v", err)
	}
	data, err := os.ReadFile(netfilterHookPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "pidof sing-box >/dev/null 2>&1 || exit 0") {
		t.Errorf("hook missing pidof guard:\n%s", body)
	}
	if !strings.Contains(body, "iptables-restore --noflush") {
		t.Errorf("hook missing restore line:\n%s", body)
	}
}

func TestWriteNetfilterHookHasScrub(t *testing.T) {
	tmp := t.TempDir()
	orig := netfilterHookPath
	netfilterHookPath = filepath.Join(tmp, "50-awgm-tproxy.sh")
	t.Cleanup(func() { netfilterHookPath = orig })

	if err := writeNetfilterHook(); err != nil {
		t.Fatalf("writeNetfilterHook: %v", err)
	}
	data, _ := os.ReadFile(netfilterHookPath)
	body := string(data)

	// Scrub block: NDMS reloads can flush one table but not the other.
	// Without scrubbing existing PREROUTING jumps before iptables-restore,
	// --noflush would append a duplicate `-A PREROUTING ... -g AWGM-*`
	// on top of the surviving one. `-[jg]` covers both legacy and current
	// jump styles for safe upgrade.
	wants := []string{
		"-[jg] AWGM-TPROXY",
		"AWGM-REDIRECT|AWGM-DNS-OFFLOAD",
		"-D PREROUTING",
	}
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("hook missing scrub fragment %q:\n%s", w, body)
		}
	}
	// Scrub must come BEFORE the restore.
	scrubIdx := strings.Index(body, "-D PREROUTING")
	restoreIdx := strings.Index(body, "iptables-restore --noflush")
	if scrubIdx < 0 || restoreIdx < 0 || scrubIdx > restoreIdx {
		t.Errorf("scrub must precede restore: scrub=%d restore=%d", scrubIdx, restoreIdx)
	}
}

func TestRemoveNetfilterRulesFile(t *testing.T) {
	tmp := t.TempDir()
	orig := netfilterRulesPath
	netfilterRulesPath = filepath.Join(tmp, "router-netfilter.rules")
	t.Cleanup(func() { netfilterRulesPath = orig })

	if err := os.WriteFile(netfilterRulesPath, []byte("dummy"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	removeNetfilterRulesFile()
	if _, err := os.Stat(netfilterRulesPath); !os.IsNotExist(err) {
		t.Errorf("expected file to be gone, got err=%v", err)
	}
	// Idempotent — second call must not panic.
	removeNetfilterRulesFile()
}

func TestRefreshNetfilterHookIfPresent(t *testing.T) {
	tmp := t.TempDir()
	orig := netfilterHookPath
	netfilterHookPath = filepath.Join(tmp, "50-awgm-tproxy.sh")
	t.Cleanup(func() { netfilterHookPath = orig })

	// No file → no-op (does not create one).
	refreshNetfilterHookIfPresent()
	if _, err := os.Stat(netfilterHookPath); !os.IsNotExist(err) {
		t.Errorf("expected no file, got err=%v", err)
	}

	// File present → rewrite with current content (and our pidof guard).
	if err := os.WriteFile(netfilterHookPath, []byte("# stale old version\n"), 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	refreshNetfilterHookIfPresent()
	data, _ := os.ReadFile(netfilterHookPath)
	if !strings.Contains(string(data), "pidof sing-box") {
		t.Errorf("expected refreshed hook with pidof, got:\n%s", data)
	}
}

func TestInstall_IdempotentOnFileExists(t *testing.T) {
	// After the runIP fix (Task 1 of wizard cleanup), stderr from `ip` is
	// appended to err.Error() via sysexec.FormatError. The substring guards
	// in Install() catch "File exists" and silently swallow the error so a
	// re-Install on already-installed routes/rules is a no-op.
	rec := newFakeExec()
	it := &IPTables{
		restoreNoflush: rec.restoreNoflush,
		runIPTables:    rec.runIPTables,
		runIP:          rec.runIP,
		persistRules:   func(string) error { return nil },
		persistHook:    func() error { return nil },
		cleanupHook:    func() {},
	}
	if err := it.Install(context.Background(), RestoreInputSpec{PolicyMark: "0xff"}); err != nil {
		t.Fatalf("first Install: %v", err)
	}

	// Simulate "File exists" failure on subsequent ip-rule/ip-route add.
	rec.runIPErr = errors.New("exit status 2 (exit 2, stderr: RTNETLINK answers: File exists)")
	if err := it.Install(context.Background(), RestoreInputSpec{PolicyMark: "0xff"}); err != nil {
		t.Fatalf("second Install (idempotent): %v", err)
	}
}

func TestBuildRestoreInput_ExpandedBypassCIDRs(t *testing.T) {
	input := buildRestoreInput(RestoreInputSpec{PolicyMark: "0xffffaaa"})

	// New CIDRs that close edge cases SKeen covered:
	// - CGNAT (RFC 6598) — ISPs deploying carrier-grade NAT
	// - 0.0.0.0/8 "this network" (RFC 1122) — never routable
	// - 192.0.0.0/24 IETF Protocol Assignments — includes NAT64 well-known
	expected := []string{
		"-A AWGM-TPROXY -d 100.64.0.0/10 -j ACCEPT",
		"-A AWGM-TPROXY -d 0.0.0.0/8 -j ACCEPT",
		"-A AWGM-TPROXY -d 192.0.0.0/24 -j ACCEPT",
		"-A AWGM-REDIRECT -d 100.64.0.0/10 -j ACCEPT",
		"-A AWGM-REDIRECT -d 0.0.0.0/8 -j ACCEPT",
		"-A AWGM-REDIRECT -d 192.0.0.0/24 -j ACCEPT",
	}
	for _, line := range expected {
		if !strings.Contains(input, line) {
			t.Errorf("missing expanded-bypass line: %q\nin:\n%s", line, input)
		}
	}
}

func TestBuildRestoreInput_DNSInterceptUDP(t *testing.T) {
	input := buildRestoreInput(RestoreInputSpec{PolicyMark: "0xffffaaa"})

	// DNS rule MUST exist in AWGM-TPROXY: -p udp --dport 53 -j TPROXY ...
	wantDNS := "-A AWGM-TPROXY -p udp --dport 53 -j TPROXY --on-port 51271 --on-ip 127.0.0.1 --tproxy-mark 0x1"
	if !strings.Contains(input, wantDNS) {
		t.Errorf("missing DNS UDP TPROXY rule\nwant: %s\ngot:\n%s", wantDNS, input)
	}

	// CRITICAL ORDERING: DNS rule MUST precede the 192.168.0.0/16 bypass.
	// Otherwise DNS-to-router-LAN-IP gets bypassed before the DNS rule fires.
	dnsIdx := strings.Index(input, wantDNS)
	bypassIdx := strings.Index(input, "-A AWGM-TPROXY -d 192.168.0.0/16 -j ACCEPT")
	if dnsIdx < 0 || bypassIdx < 0 {
		t.Fatalf("DNS or bypass rule not found")
	}
	if dnsIdx > bypassIdx {
		t.Errorf("DNS rule at offset %d must precede 192.168/16 bypass at offset %d", dnsIdx, bypassIdx)
	}
}

func TestBuildRestoreInput_DNSInterceptTCP(t *testing.T) {
	input := buildRestoreInput(RestoreInputSpec{PolicyMark: "0xffffaaa"})

	// TCP DNS rule MUST exist in AWGM-REDIRECT.
	wantDNS := "-A AWGM-REDIRECT -p tcp --dport 53 -j REDIRECT --to-ports 51272"
	if !strings.Contains(input, wantDNS) {
		t.Errorf("missing DNS TCP REDIRECT rule\nwant: %s\ngot:\n%s", wantDNS, input)
	}

	// Ordering: DNS rule MUST precede the 192.168/16 bypass in AWGM-REDIRECT.
	dnsIdx := strings.Index(input, wantDNS)
	bypassIdx := strings.Index(input, "-A AWGM-REDIRECT -d 192.168.0.0/16 -j ACCEPT")
	if dnsIdx < 0 || bypassIdx < 0 {
		t.Fatalf("DNS or bypass rule not found")
	}
	if dnsIdx > bypassIdx {
		t.Errorf("TCP DNS rule at offset %d must precede 192.168/16 bypass at offset %d", dnsIdx, bypassIdx)
	}
}

func TestBuildRestoreInput_WANIPsRendered(t *testing.T) {
	// Synthetic RFC 5737 TEST-NET-3 + RFC 1918 — mirrors a real multi-WAN
	// router with public WAN + tunnel addresses.
	spec := RestoreInputSpec{
		PolicyMark: "0xffffaaa",
		WANIPs:     []string{"203.0.113.207/32", "10.8.1.3/32"},
	}
	input := buildRestoreInput(spec)

	// WAN-IP rules MUST appear in BOTH chains.
	expected := []string{
		"-A AWGM-TPROXY -d 203.0.113.207/32 -j ACCEPT",
		"-A AWGM-TPROXY -d 10.8.1.3/32 -j ACCEPT",
		"-A AWGM-REDIRECT -d 203.0.113.207/32 -j ACCEPT",
		"-A AWGM-REDIRECT -d 10.8.1.3/32 -j ACCEPT",
	}
	for _, line := range expected {
		if !strings.Contains(input, line) {
			t.Errorf("missing WAN-IP line: %q\nin:\n%s", line, input)
		}
	}
}

func TestBuildRestoreInput_EmptyWANIPs_NoExclusions(t *testing.T) {
	spec := RestoreInputSpec{PolicyMark: "0xffffaaa", WANIPs: nil}
	input := buildRestoreInput(spec)

	// No /32 host-routes should appear other than 255.255.255.255/32.
	for _, line := range strings.Split(input, "\n") {
		if strings.Contains(line, "/32 -j ACCEPT") && !strings.Contains(line, "255.255.255.255") {
			t.Errorf("unexpected /32 exclusion when WANIPs empty: %s", line)
		}
	}
}
