# Project Rules & Strict Constraints for Antigravity

## 1. ABSOLUTE BAN: NEVER USE `--force-reinstall`
- **STRICT PROHIBITION:** NEVER run or generate commands or scripts with `opkg install --force-reinstall`.
- **CRITICAL REASON:** When `opkg` runs with `--force-reinstall`, it calls `prerm` without passing `ACTION=upgrade`. Any package scripts treat this as uninstallation, stopping services, wiping `managedServers` (`Wireguard2` / AWGM), deleting NDMS interfaces, killing tunnels, and terminating proxy listeners (like port 1099 for Happ / Xray).
- **APPROVED DEPLOYMENT METHOD:**
  - Always use: `opkg install --force-downgrade --force-overwrite <package.ipk>`
  - NEVER include `--force-reinstall` in any shell command, Python script, scratch script, or deployment pipeline.

## 2. ABSOLUTE BAN: NEVER EXECUTE `--cleanup` AUTOMATICALLY
- `/opt/bin/awg-manager --cleanup` is a destructive manual recovery tool designed ONLY for complete device wipe.
- It MUST NEVER be placed in `prerm`, `postrm`, automated scripts, or deploy scripts.

## 3. WORKSPACE & REPOSITORY INTEGRITY
- **Authoritative Workspace:** `e:\AWGM\awg-manager` on branch `feature/mihomo-ai-proxyrt`.
- **STRICT PROHIBITION:** NEVER edit, deploy from, or touch `e:\AWGM\awg-manager-mihomo`.
- Router IP: `192.168.90.1` (Home Keenetic).

## 4. ROUTING & SERVER INTEGRITY
- The active routing engine is **Mihomo**.
- Inbound port `1099` (`mihomoMixedPort: 1099`) MUST remain active in `settings.json` so that Xray VLESS (Happ proxy on phone) and local proxy instances can route traffic.
- Interface `Wireguard2` (server AWGM, port 51820, peers `FreeTurnDacha` and `Dacha`) MUST NOT be deleted or unconfigured.
- `telemt` listener MUST bind to `0.0.0.0:8443` so it is accessible from LAN Wi-Fi as well as WAN.
