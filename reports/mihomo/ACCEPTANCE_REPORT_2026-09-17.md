# Final Acceptance & Verification Report: Mihomo Core Stabilization

- **Date:** 2026-09-17
- **Target Environment:** Keenetic Ultra (KN-1812 / NC-1812, aarch64, KeeneticOS 5.01.C.0.0-1, IP `192.168.90.1`)
- **Package Version:** `2.17.49` (`awg-manager_2.17.49_aarch64-3.10-kn.ipk`)
- **Author:** Antigravity / AI Lead Engineer
- **Branch:** `feature/mihomo-ai-proxyrt`

---

## 1. Executive Summary

All 6 stages of the **Mihomo Core Stabilization Plan** (`reports/mihomo/MIHOMO_CORE_STABILIZATION_PLAN_2026-09-14.md`) have been completed, compiled into release version `2.17.49`, deployed to the authoritative router `192.168.90.1` using the approved safe deployment method, and verified against the acceptance matrix.

- **Zero-Disruption Deployment:** Upgraded from `2.17.47` to `2.17.49` via `opkg install --force-downgrade --force-overwrite`. No tunnels were dropped, and interface `Wireguard2` remained continuously up and connected.
- **Strict Compliance:** `--force-reinstall` and `--cleanup` were NOT used. Inbound mixed port `1099` remained active throughout. `telemt` remained bound to `0.0.0.0:8443`.
- **Defunct / Zombie Fix Verified:** Previous defunct zombie process `[mihomo] <defunct>` was completely eradicated. Process supervision correctly reaps and manages child processes.
- **Traffic Interception Verified:** TProxy on UDP port `51271` and REDIRECT on TCP port `51272` actively intercepting and counting traffic. Real HTTP proxy traffic through port `1099` verified (`detectportal.firefox.com` -> 200 OK).

---

## 2. Acceptance Matrix Verification (§ 5)

| # | Acceptance Matrix Item | Verification Method | Status | Notes |
|---|------------------------|---------------------|--------|-------|
| 1 | **Pre-deployment backup & snapshot** | Router local backup to `/opt/tmp/pre_acceptance_backup_2.17.49/` | **PASS** | settings, mihomo configs, sing-box slots, and iptables dump preserved |
| 2 | **Binary & Package Version** | `/opt/bin/awg-manager -version` | **PASS** | `awg-manager version 2.17.49` |
| 3 | **Process Supervision** | `ps w \| grep -E 'awg-manager\|mihomo\|telemt'` | **PASS** | `awg-manager` (PID 19879), `mihomo` (PID 20493), `telemt` (PID 19795), `wdtt-server` (PID 22290), `sing-box` (PID 23426) |
| 4 | **Zero Defunct / Zombie Processes** | Process table inspection | **PASS** | No `<defunct>` or orphan processes found |
| 5 | **Port 1099 (Mixed HTTP/SOCKS5 Inbound)** | Live HTTP proxy request | **PASS** | `http://detectportal.firefox.com/success.txt` -> 200 OK |
| 6 | **Port 9090 (Clash Controller)** | `/api/mihomo/clash/proxies` | **PASS** | 50 proxies / outbounds loaded in active memory |
| 7 | **Port 8443 (Telemt MTProxy)** | TCP handshake | **PASS** | Bound to `0.0.0.0:8443`, handshake succeeded |
| 8 | **Port 2222 (Web UI)** | HTTP GET `/` & `/routing` | **PASS** | Both return HTTP 200, HTML rendered correctly |
| 9 | **Port 51271 (TProxy UDP)** | Netfilter counter check | **PASS** | `AWGM-TPROXY`: 55 pkts, 13,845 bytes redirected to 127.0.0.1:51271 |
| 10 | **Port 51272 (Redirect TCP)** | Netfilter counter check | **PASS** | `AWGM-REDIRECT`: 6 pkts, 360 bytes redirected to port 51272 |
| 11 | **Wireguard2 Interface Preservation** | `ndmc -c 'show interface Wireguard2'` | **PASS** | `id: Wireguard2`, `connected: yes`, `state: up` with both peers intact |
| 12 | **DNS Resolution** | Router port 53 query | **PASS** | `keenetic.com` resolved to `49.12.59.2` |
| 13 | **Mihomo Native API Endpoints** | Authenticated session API checks | **PASS** | `/api/mihomo/status`, `/api/mihomo/native/rules`, `/api/mihomo/clash/proxies` |
| 14 | **Atomic Reload Resilience** | POST `/api/mihomo/reload` | **PASS** | Returns `{"success": true, "data": {"reloaded": true}}`; smooth handover to new PID without socket collisions |
| 15 | **System Info Consistency** | GET `/api/system/info` | **PASS** | Reports `version: "2.17.49"`, `routingEngine: "mihomo"`, `kernelModuleLoaded: true` |

---

## 3. Netfilter Chains Verified on Live Router

### `AWGM-TPROXY` (Mangle PREROUTING)
```
Chain AWGM-TPROXY (1 references)
 pkts bytes target     prot opt in     out     source        destination         
   55 13845 TPROXY     udp  --  *      *       0.0.0.0/0     0.0.0.0/0     TPROXY redirect 127.0.0.1:51271 mark 0x1/0xffffffff
```

### `AWGM-REDIRECT` (Nat PREROUTING)
```
Chain AWGM-REDIRECT (1 references)
 pkts bytes target     prot opt in     out     source        destination         
    6   360 REDIRECT   tcp  --  *      *       0.0.0.0/0     0.0.0.0/0     redir ports 51272
```

---

## 4. Artifacts & Staging Summary

All resolution reports for the entire stabilization cycle:
1. `reports/mihomo/GATE1_PRO_FINAL_BLOCKER_RESOLUTION_2026-09-16.md`
2. `reports/mihomo/GATE2_RESOLUTION_REPORT_2026-09-16.md`
3. `reports/mihomo/GATE3_RESOLUTION_REPORT_2026-09-17.md`
4. `reports/mihomo/GATE4_RESOLUTION_REPORT_2026-09-17.md`
5. `reports/mihomo/STAGE3_RESOLUTION_REPORT_2026-09-17.md`
6. `reports/mihomo/STAGE4_RESOLUTION_REPORT_2026-09-17.md`
7. `reports/mihomo/STAGE5_RESOLUTION_REPORT_2026-09-17.md`
8. `reports/mihomo/STAGE6_RESOLUTION_REPORT_2026-09-17.md`
9. `reports/mihomo/ACCEPTANCE_REPORT_2026-09-17.md`

All automated unit and race tests pass:
- `internal/singbox/router`: PASS
- `internal/mihomo`: PASS
- `internal/mihomonative`: PASS
- `frontend`: 0 errors (`npm run check`)
- `git diff --check`: 0 errors
