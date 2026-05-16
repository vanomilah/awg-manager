package router

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	sysexec "github.com/hoaxisr/awg-manager/internal/sys/exec"
	sysiptables "github.com/hoaxisr/awg-manager/internal/sys/iptables"
	"github.com/hoaxisr/awg-manager/internal/sys/osdetect"
)

const (
	TPROXYPort   = 51271
	// RedirectPort is sing-box's REDIRECT inbound for TCP. We split TCP
	// onto NAT REDIRECT (instead of mangle TPROXY) because TPROXY for
	// TCP requires a working `-m socket --transparent` bypass to deliver
	// ACK/data on already-established connections — Keenetic's 4.9-ndm-5
	// kernel evaluates that match as 0 hits, so ACKs would re-enter the
	// TPROXY rule, get redirected to 127.0.0.1:51271 with the listener
	// destination, and the kernel would emit RST because no socket
	// matches that 4-tuple. NAT REDIRECT sidesteps the issue entirely:
	// conntrack tracks the DNAT for established flows, ACKs are
	// auto-translated, and sing-box's accept()ed socket handles them
	// like any normal TCP connection. SKeen ships the same split.
	RedirectPort  = 51272
	Fwmark        = 0x1
	RoutingTable  = 100
	ChainName     = "AWGM-TPROXY"
	RedirectChain = "AWGM-REDIRECT"
	// IPRulePriority is the fixed `ip rule` priority for our fwmark rule.
	// Above NDMS policy rules (~100-200) and below system main/default
	// (32766/32767). Hard-coded so Install is fully idempotent and so
	// our rule never accidentally displaces the kernel's local-table
	// rule at priority 0.
	IPRulePriority = 30000
	// maxIPRuleDrainPasses caps the drain loop in Install/Uninstall.
	// Defensive bound — `ip rule del` should return ENOENT quickly when
	// nothing matches, but a buggy kernel returning success forever
	// would otherwise hang Install. 32 is well above any realistic
	// duplicate count (the worst observed was 10 in the wild).
	maxIPRuleDrainPasses = 32
)

// Mutable in tests via t.Cleanup so they can redirect into a tmp dir.
// Production code reads these at call time.
var (
	netfilterHookPath  = "/opt/etc/ndm/netfilter.d/50-awgm-tproxy.sh"
	netfilterRulesPath = "/opt/etc/awg-manager/singbox/router-netfilter.rules"
)

func kernelModuleName() string { return "xt_TPROXY" }

func buildTProxyModulePath(kernelVersion string) string {
	return filepath.Join("/lib/modules", kernelVersion, "xt_TPROXY.ko")
}

func isModuleLoaded(name string) bool {
	data, err := os.ReadFile("/proc/modules")
	if err != nil {
		return false
	}
	prefix := name + " "
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

var (
	netfilterOnce      sync.Once
	netfilterAvailable bool
)

func IsNetfilterAvailable() bool {
	netfilterOnce.Do(func() {
		if isModuleLoaded(kernelModuleName()) {
			netfilterAvailable = true
			return
		}
		kernel := osdetect.KernelRelease()
		if kernel == "" {
			return
		}
		_, err := os.Stat(buildTProxyModulePath(kernel))
		netfilterAvailable = err == nil
	})
	return netfilterAvailable
}

func EnsureTProxyModule(ctx context.Context) error {
	if isModuleLoaded(kernelModuleName()) {
		return nil
	}
	kernel := osdetect.KernelRelease()
	if kernel == "" {
		return ErrNetfilterComponentMissing
	}
	path := buildTProxyModulePath(kernel)
	if _, err := os.Stat(path); err != nil {
		return ErrNetfilterComponentMissing
	}
	if _, err := sysexec.Run(ctx, "insmod", path); err != nil {
		return fmt.Errorf("insmod %s: %w", path, err)
	}
	return nil
}

var (
	tproxyTargetOnce   sync.Once
	tproxyTargetResult bool
)

func IsTProxyTargetAvailable(ctx context.Context) bool {
	tproxyTargetOnce.Do(func() {
		res, err := sysexec.Run(ctx, sysiptables.Binary, "-j", "TPROXY", "--help")
		if err != nil || res == nil {
			return
		}
		tproxyTargetResult = strings.Contains(res.Stdout+res.Stderr, "TPROXY")
	})
	return tproxyTargetResult
}

type RestoreInputSpec struct {
	// PolicyMark is the NDMS-assigned mark (hex, e.g. "0xffffaaa") that
	// NDMS sets on connections from devices bound to the chosen access
	// policy. Empty means no PREROUTING jump (defensive — caller should
	// never reach Install with empty mark, but iptables doesn't panic).
	PolicyMark string

	// WANIPs is a list of router-owned IP addresses (in "X.X.X.X/32" form)
	// that must NOT be TPROXY'd: traffic from LAN to the router's own
	// public-WAN or tunnel-endpoint IPs would otherwise loop back into
	// sing-box. Collected dynamically by WANIPCollector before Install.
	// Empty list = no extra exclusions (router still works, just exposes
	// the WAN-IP loop edge case).
	WANIPs []string
}

var bypassCIDRs = []string{
	"127.0.0.0/8",
	"169.254.0.0/16",
	"100.64.0.0/10",  // CGNAT (RFC 6598)
	"0.0.0.0/8",      // this network (RFC 1122)
	"192.0.0.0/24",   // IETF Protocol Assignments (NAT64 well-known)
	"224.0.0.0/4",
	"255.255.255.255/32",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
}

func buildRestoreInput(spec RestoreInputSpec) string {
	var b strings.Builder

	// ---- *mangle table: UDP via TPROXY ----
	//
	// PREROUTING entry uses `-A` (append) so it runs AFTER the NDMS
	// _NDM_*_PREROUTING_* chains. The previous `-I PREROUTING 1` placed
	// us first, before _NDM_DNSRT_PREROUTING_MANGLE (which matches
	// `mark match 0x0`) had a chance to handle no-mark-yet UDP — the
	// no-mark path triggered MASQUERADE through FORWARD/POSTROUTING,
	// conntrack recorded has_nat=true, and from then on every packet of
	// the flow reached sing-box's tproxy-in with WAN IP source.
	// Empirically confirmed 2026-05-16 against SKeen's working setup,
	// which uses the same append pattern.
	//
	// Policy/direction filters live INSIDE the chain (SKeen-style),
	// because the jump itself is now unconditional. Bypass exemptions
	// use `-j ACCEPT` (not RETURN): with `-g` on the entry, RETURN would
	// unwind back to PREROUTING and let the bypass'd packet hit further
	// NDMS rules; ACCEPT terminates the mangle table cleanly.
	b.WriteString("*mangle\n")
	fmt.Fprintf(&b, ":%s - [0:0]\n", ChainName)

	if spec.PolicyMark != "" {
		// Exit early when packet doesn't belong to our access policy.
		fmt.Fprintf(&b, "-A %s -m connmark ! --mark %s -j ACCEPT\n", ChainName, spec.PolicyMark)
	}
	// Reply direction: NDMS's connmark applies to BOTH directions of the
	// conntrack entry, so return packets from the internet would also
	// match the policy mark above. ACCEPT them out before TPROXY.
	fmt.Fprintf(&b, "-A %s -m conntrack --ctdir REPLY -j ACCEPT\n", ChainName)

	// DNS intercept MUST precede the dst-based ACCEPTs below: LAN clients
	// configured with DNS=router-LAN-IP (192.168.1.1 etc.) would otherwise
	// hit the 192.168.0.0/16 ACCEPT first, leaking DNS into NDMS-resolver.
	fmt.Fprintf(&b, "-A %s -p udp --dport 53 -j TPROXY --on-port %d --on-ip 127.0.0.1 --tproxy-mark 0x%x\n",
		ChainName, TPROXYPort, Fwmark)

	for _, cidr := range bypassCIDRs {
		fmt.Fprintf(&b, "-A %s -d %s -j ACCEPT\n", ChainName, cidr)
	}
	for _, ip := range spec.WANIPs {
		fmt.Fprintf(&b, "-A %s -d %s -j ACCEPT\n", ChainName, ip)
	}
	fmt.Fprintf(&b, "-A %s -m mark --mark 0xff -j ACCEPT\n", ChainName)
	fmt.Fprintf(&b, "-A %s -p udp -j TPROXY --on-port %d --on-ip 127.0.0.1 --tproxy-mark 0x%x\n",
		ChainName, TPROXYPort, Fwmark)

	if spec.PolicyMark != "" {
		fmt.Fprintf(&b, "-A PREROUTING -p udp -m conntrack ! --ctstate INVALID -g %s\n", ChainName)
	}
	b.WriteString("COMMIT\n")

	// ---- *nat table: TCP via REDIRECT ----
	// REDIRECT is a NAT operation; conntrack records the DNAT for the
	// SYN and auto-applies it to subsequent packets. Established TCP
	// flows therefore route to sing-box's ACCEPT()ed socket without
	// re-evaluating our PREROUTING jump, sidestepping the
	// transparent-socket-lookup failure that breaks pure-TPROXY TCP
	// on this kernel. Same `-A` + `-g` + filters-inside pattern as mangle.
	b.WriteString("*nat\n")
	fmt.Fprintf(&b, ":%s - [0:0]\n", RedirectChain)

	if spec.PolicyMark != "" {
		fmt.Fprintf(&b, "-A %s -m connmark ! --mark %s -j ACCEPT\n", RedirectChain, spec.PolicyMark)
	}
	fmt.Fprintf(&b, "-A %s -m conntrack --ctdir REPLY -j ACCEPT\n", RedirectChain)

	// DNS-перехват on TCP — same ordering rationale as mangle chain.
	fmt.Fprintf(&b, "-A %s -p tcp --dport 53 -j REDIRECT --to-ports %d\n", RedirectChain, RedirectPort)

	for _, cidr := range bypassCIDRs {
		fmt.Fprintf(&b, "-A %s -d %s -j ACCEPT\n", RedirectChain, cidr)
	}
	for _, ip := range spec.WANIPs {
		fmt.Fprintf(&b, "-A %s -d %s -j ACCEPT\n", RedirectChain, ip)
	}
	// Bypass router admin port so we don't redirect our own UI traffic.
	fmt.Fprintf(&b, "-A %s -p tcp --dport 79 -j ACCEPT\n", RedirectChain)
	fmt.Fprintf(&b, "-A %s -p tcp -j REDIRECT --to-ports %d\n", RedirectChain, RedirectPort)

	if spec.PolicyMark != "" {
		fmt.Fprintf(&b, "-A PREROUTING -p tcp -m conntrack ! --ctstate INVALID -g %s\n", RedirectChain)
	}
	b.WriteString("COMMIT\n")
	return b.String()
}

type restoreNoflushFn func(ctx context.Context, input string) error
type runFn func(ctx context.Context, args ...string) error

type persistFn func(input string) error

type IPTables struct {
	restoreNoflush restoreNoflushFn
	runIPTables    runFn
	runIP          runFn
	persistRules   persistFn
	persistHook    func() error
	cleanupHook    func()
}

func NewIPTables() *IPTables {
	return &IPTables{
		restoreNoflush: sysiptables.RestoreNoflush,
		runIPTables:    sysiptables.Run,
		runIP: func(ctx context.Context, args ...string) error {
			result, err := sysexec.Run(ctx, "ip", args...)
			return sysexec.FormatError(result, err)
		},
		persistRules: writeNetfilterRulesFile,
		persistHook:  writeNetfilterHook,
		cleanupHook:  removeNetfilterRulesFile,
	}
}

func (it *IPTables) Install(ctx context.Context, spec RestoreInputSpec) error {
	// Scrub any existing PREROUTING jumps to AWGM-TPROXY before inserting
	// the new one. iptables-restore --noflush + -I PREROUTING 1 would
	// otherwise stack a duplicate jump on every restart / mark-change /
	// re-Enable: the stale rule from the previous policy/mark survives
	// because mangle isn't flushed, and the new rule lands in front of it.
	// Idempotent: a no-op when no prior jumps exist.
	it.removeSourceHooks(ctx)

	input := buildRestoreInput(spec)
	if it.persistRules != nil {
		if err := it.persistRules(input); err != nil {
			return fmt.Errorf("write netfilter rules: %w", err)
		}
	}
	if it.persistHook != nil {
		if err := it.persistHook(); err != nil {
			return fmt.Errorf("write netfilter hook: %w", err)
		}
	}
	if err := it.restoreNoflush(ctx, input); err != nil {
		return fmt.Errorf("iptables-restore: %w", err)
	}
	// Drain ALL existing fwmark rules pointing at our table before
	// adding a fresh one. Without this, every Install (Reconcile,
	// daemon restart, mark-change, re-Enable) leaves a duplicate
	// because `ip rule add` without explicit priority lands at
	// previous_priority + 1 instead of being deduped — and a stack of
	// rules at priorities 0-N displaces the kernel's `from all lookup
	// local` rule (normally at prio 0), breaking router-local routing
	// (sing-box outbounds to direct silently fail).
	for i := 0; i < maxIPRuleDrainPasses; i++ {
		if err := it.runIP(ctx, "rule", "del", "fwmark", fmt.Sprintf("0x%x", Fwmark),
			"table", fmt.Sprintf("%d", RoutingTable)); err != nil {
			break
		}
	}
	// Use an explicit priority well above NDMS policy rules (100-200)
	// and well below the system main/default tables (32766/32767), so
	// our rule is identifiable and idempotent.
	if err := it.runIP(ctx, "rule", "add", "fwmark", fmt.Sprintf("0x%x", Fwmark),
		"table", fmt.Sprintf("%d", RoutingTable),
		"priority", fmt.Sprintf("%d", IPRulePriority)); err != nil {
		if !strings.Contains(err.Error(), "File exists") {
			return fmt.Errorf("ip rule add: %w", err)
		}
	}
	if err := it.runIP(ctx, "route", "add", "local", "0.0.0.0/0", "dev", "lo",
		"table", fmt.Sprintf("%d", RoutingTable)); err != nil {
		if !strings.Contains(err.Error(), "File exists") {
			return fmt.Errorf("ip route add: %w", err)
		}
	}
	return nil
}

func writeNetfilterRulesFile(input string) error {
	if err := os.MkdirAll(filepath.Dir(netfilterRulesPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(netfilterRulesPath, []byte(input), 0644)
}

func writeNetfilterHook() error {
	if err := os.MkdirAll(filepath.Dir(netfilterHookPath), 0755); err != nil {
		return err
	}
	// Scrub block before restore: when NDMS reloads only one table (e.g.
	// nat) but leaves mangle intact, restoring --noflush would append a
	// SECOND PREROUTING jump to AWGM-TPROXY on top of the surviving one.
	// `-[jg]` regex covers both legacy `-j` and current `-g` syntax so
	// the upgrade path doesn't leave duplicates around.
	script := fmt.Sprintf(`#!/bin/sh
[ "$type" = "ip6tables" ] && exit 0
case "$table" in mangle|nat) ;; *) exit 0 ;; esac
[ -f %[1]q ] || exit 0
pidof sing-box >/dev/null 2>&1 || exit 0
mangle_ok=0; nat_ok=0
/opt/sbin/iptables -w -t mangle -nL %[2]s >/dev/null 2>&1 && mangle_ok=1
/opt/sbin/iptables -w -t nat    -nL %[6]s >/dev/null 2>&1 && nat_ok=1
if [ "$mangle_ok" -eq 0 ] || [ "$nat_ok" -eq 0 ]; then
  /opt/sbin/iptables -w -t mangle -S PREROUTING 2>/dev/null \
    | grep -E -- '-[jg] %[2]s($| )' \
    | sed 's/-A PREROUTING/-D PREROUTING/' \
    | while IFS= read -r line; do /opt/sbin/iptables -w -t mangle $line 2>/dev/null; done
  /opt/sbin/iptables -w -t nat -S PREROUTING 2>/dev/null \
    | grep -E -- '-[jg] %[6]s($| )' \
    | sed 's/-A PREROUTING/-D PREROUTING/' \
    | while IFS= read -r line; do /opt/sbin/iptables -w -t nat $line 2>/dev/null; done
  /opt/sbin/iptables-restore --noflush < %[1]q
  /opt/sbin/ip rule add fwmark 0x%[3]x table %[4]d priority %[5]d 2>/dev/null || true
  /opt/sbin/ip route add local 0.0.0.0/0 dev lo table %[4]d 2>/dev/null || true
  logger -t awgm-tproxy "netfilter.d: restored AWGM chains after NDMS reload"
fi
`, netfilterRulesPath, ChainName, Fwmark, RoutingTable, IPRulePriority, RedirectChain)
	return os.WriteFile(netfilterHookPath, []byte(script), 0755)
}

// removeNetfilterRulesFile deletes the persisted rules file so the
// netfilter.d hook becomes a no-op on the next NDMS reload. Called on
// engine Disable. Idempotent.
func removeNetfilterRulesFile() {
	_ = os.Remove(netfilterRulesPath)
}

// refreshNetfilterHookIfPresent rewrites the netfilter.d hook script
// when one is already installed, so older versions get the current
// pidof guard on daemon startup. No-op when the file is absent —
// Install creates it on first Enable.
func refreshNetfilterHookIfPresent() {
	if _, err := os.Stat(netfilterHookPath); err != nil {
		return
	}
	_ = writeNetfilterHook()
}

func (it *IPTables) Uninstall(ctx context.Context) error {
	if it.cleanupHook != nil {
		it.cleanupHook()
	}
	it.removeSourceHooks(ctx)
	_ = it.runIPTables(ctx, "-t", "mangle", "-F", ChainName)
	_ = it.runIPTables(ctx, "-t", "mangle", "-X", ChainName)
	_ = it.runIPTables(ctx, "-t", "nat", "-F", RedirectChain)
	_ = it.runIPTables(ctx, "-t", "nat", "-X", RedirectChain)
	// Drain ALL fwmark rules — historically Install accumulated
	// duplicates at priorities 0-N (auto-assigned), so a single `del`
	// would leave the rest. Loop until ENOENT, capped defensively.
	for i := 0; i < maxIPRuleDrainPasses; i++ {
		if err := it.runIP(ctx, "rule", "del", "fwmark", fmt.Sprintf("0x%x", Fwmark),
			"table", fmt.Sprintf("%d", RoutingTable)); err != nil {
			break
		}
	}
	_ = it.runIP(ctx, "route", "flush", "table", fmt.Sprintf("%d", RoutingTable))
	return nil
}

func (it *IPTables) removeSourceHooks(ctx context.Context) {
	it.removeSourceHooksFromTable(ctx, "mangle", ChainName)
	it.removeSourceHooksFromTable(ctx, "nat", RedirectChain)
}

func (it *IPTables) removeSourceHooksFromTable(ctx context.Context, table, chain string) {
	result, err := sysexec.Run(ctx, sysiptables.Binary, "-w", "-t", table, "-S", "PREROUTING")
	if err != nil || result == nil {
		return
	}
	// Match both `-j chain` (old jump syntax pre-fastnat-fix) and
	// `-g chain` (current goto syntax) so upgrading installs scrub
	// stale jumps from previous versions before we re-append the new one.
	jumpJ := "-j " + chain
	gotoG := "-g " + chain
	for _, line := range strings.Split(result.Stdout, "\n") {
		if !strings.Contains(line, jumpJ) && !strings.Contains(line, gotoG) {
			continue
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "-A PREROUTING") {
			continue
		}
		deleteLine := strings.Replace(line, "-A PREROUTING", "-D PREROUTING", 1)
		args := append([]string{"-t", table}, strings.Fields(deleteLine)...)
		_ = it.runIPTables(ctx, args...)
	}
}

func (it *IPTables) IsInstalled(ctx context.Context) bool {
	return it.runIPTables(ctx, "-t", "mangle", "-nL", ChainName) == nil
}

