import sys
import paramiko
import os
import glob
import json
import time

def deploy():
    host = "192.168.90.1"
    port = 22
    user = "root"
    password = "Qaz74251106"

    client = paramiko.SSHClient()
    client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    
    print("Connecting to main router...", flush=True)
    client.connect(host, port=port, username=user, password=password)

    stdin, stdout, stderr = client.exec_command("uname -m")
    arch = stdout.read().decode().strip()
    print(f"Router architecture: {arch}", flush=True)

    entware_arch = ""
    if arch == "aarch64":
        entware_arch = "aarch64-3.10"
    elif "mips" in arch:
        entware_arch = "mipsel-3.4"
    else:
        entware_arch = "mipsel-3.4"
    
    print(f"Target Entware arch: {entware_arch}", flush=True)

    if len(sys.argv) > 1 and sys.argv[1] == "apply-mihomo":
        command = (
            "/opt/bin/curl -sS --max-time 30 -X POST "
            "-w '\\nhttp=%{http_code}\\n' http://127.0.0.1:2222/api/mihomo/reload; sleep 3"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "restart-engines":
        command = (
            "pid=$(pidof sing-box 2>/dev/null || true); "
            "[ -z \"$pid\" ] || kill -HUP $pid; "
            "sleep 2; /opt/etc/init.d/S99awg-manager restart; sleep 4"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "cleanup-stale-mihomo":
        command = (
            "owner=$(netstat -lntp 2>/dev/null | awk '/127.0.0.1:9090/{split($7,a,\"/\"); print a[1]; exit}'); "
            "for pid in $(pidof mihomo 2>/dev/null); do "
            "if [ -z \"$owner\" ] || [ \"$pid\" != \"$owner\" ]; then kill $pid; echo killed-stale:$pid; fi; done"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "probe-mihomo":
        command = (
            "timeout 10 /opt/etc/awg-manager/mihomo/mihomo "
            "-d /opt/etc/awg-manager/mihomo 2>&1 | "
            "grep -E 'level=(error|fatal)|configuration|listener|Start initial|Initial configuration' | tail -n 30"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "sniffer-diag":
        command = "grep -A18 '^sniffer:' /opt/etc/awg-manager/mihomo/config.yaml 2>&1 || true"
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "geo-assets":
        command = (
            "find /opt/etc/awg-manager/mihomo -maxdepth 2 -type f "
            "\\( -iname '*geoip*' -o -iname '*geosite*' -o -iname '*mmdb*' \\) "
            "-exec ls -lh {} \\; 2>&1"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip() or "no-geo-assets", flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "enable-mihomo-udp-test":
        command = (
            "iptables -t mangle -C AWGM-TPROXY -p udp -j TPROXY "
            "--on-port 51271 --on-ip 127.0.0.1 --tproxy-mark 0x1 2>/dev/null || "
            "iptables -t mangle -A AWGM-TPROXY -p udp -j TPROXY "
            "--on-port 51271 --on-ip 127.0.0.1 --tproxy-mark 0x1; "
            "iptables -t mangle -L AWGM-TPROXY -n -v --line-numbers | tail -n 5"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "disable-mihomo-udp-test":
        command = (
            "while iptables -t mangle -C AWGM-TPROXY -p udp -j TPROXY "
            "--on-port 51271 --on-ip 127.0.0.1 --tproxy-mark 0x1 2>/dev/null; do "
            "iptables -t mangle -D AWGM-TPROXY -p udp -j TPROXY "
            "--on-port 51271 --on-ip 127.0.0.1 --tproxy-mark 0x1; done; "
            "iptables -t mangle -L AWGM-TPROXY -n -v --line-numbers | tail -n 5"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "bypass-mihomo-dns-test":
        command = (
            "while iptables -t mangle -C AWGM-TPROXY -p udp --dport 53 -j TPROXY "
            "--on-port 1053 --on-ip 127.0.0.1 --tproxy-mark 0x1 2>/dev/null; do "
            "iptables -t mangle -D AWGM-TPROXY -p udp --dport 53 -j TPROXY "
            "--on-port 1053 --on-ip 127.0.0.1 --tproxy-mark 0x1; done; "
            "while iptables -t mangle -C AWGM-TPROXY -p udp -j TPROXY "
            "--on-port 51271 --on-ip 127.0.0.1 --tproxy-mark 0x1 2>/dev/null; do "
            "iptables -t mangle -D AWGM-TPROXY -p udp -j TPROXY "
            "--on-port 51271 --on-ip 127.0.0.1 --tproxy-mark 0x1; done; "
            "iptables -t mangle -L AWGM-TPROXY -n -v --line-numbers | tail -n 6"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "block-policy-quic-test":
        command = (
            "iptables -t mangle -C AWGM-TPROXY -p udp --dport 443 -j DROP 2>/dev/null || "
            "iptables -t mangle -A AWGM-TPROXY -p udp --dport 443 -j DROP; "
            "iptables -t mangle -L AWGM-TPROXY -n -v --line-numbers | tail -n 4"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "unblock-policy-quic-test":
        command = (
            "while iptables -t mangle -C AWGM-TPROXY -p udp --dport 443 -j DROP 2>/dev/null; do "
            "iptables -t mangle -D AWGM-TPROXY -p udp --dport 443 -j DROP; done; "
            "iptables -t mangle -L AWGM-TPROXY -n -v --line-numbers | tail -n 4"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "add-telegram-cidrs":
        config_path = "/opt/etc/awg-manager/singbox/config.d/disabled/20-router.json"
        telegram_cidrs = [
            "91.105.192.0/23",
            "91.108.4.0/22",
            "91.108.8.0/21",
            "91.108.16.0/21",
            "91.108.56.0/22",
            "95.161.64.0/20",
            "149.154.160.0/20",
            "185.76.151.0/24",
            "2001:67c:4e8::/48",
            "2001:b28:f23c::/47",
            "2001:b28:f23f::/48",
            "2a0a:f280::/32",
        ]
        stdin, stdout, stderr = client.exec_command("cat " + config_path)
        raw_config = stdout.read().decode("utf-8", "replace")
        read_error = stderr.read().decode("utf-8", "replace").strip()
        if read_error or not raw_config:
            raise RuntimeError(f"read router config failed: {read_error or 'empty file'}")
        config = json.loads(raw_config)
        rules = config.setdefault("route", {}).setdefault("rules", [])
        marker = set(telegram_cidrs)
        existing = next(
            (
                rule
                for rule in rules
                if set(rule.get("ip_cidr") or []) == marker
                and rule.get("outbound") == "Block"
            ),
            None,
        )
        if existing is None:
            telegram_rule = {
                "ip_cidr": telegram_cidrs,
                "action": "route",
                "outbound": "Block",
            }
            insert_at = next(
                (
                    index
                    for index, rule in enumerate(rules)
                    if "geosite-telegram" in (rule.get("rule_set") or [])
                ),
                len(rules),
            )
            rules.insert(insert_at, telegram_rule)
            backup_path = config_path + ".before-telegram-cidrs"
            temp_path = config_path + ".tmp"
            payload = (json.dumps(config, ensure_ascii=False, indent=2) + "\n").encode("utf-8")
            write_command = (
                f"[ -e {backup_path} ] || cp {config_path} {backup_path}; "
                f"umask 022; tee {temp_path} >/dev/null && mv {temp_path} {config_path}"
            )
            stdin, stdout, stderr = client.exec_command(write_command)
            stdin.write(payload)
            stdin.channel.shutdown_write()
            write_output = stdout.read().decode("utf-8", "replace")
            write_error = stderr.read().decode("utf-8", "replace")
            status = stdout.channel.recv_exit_status()
            if status != 0:
                raise RuntimeError(f"write router config failed: {write_output}{write_error}")
            print("telegram-cidrs-added", flush=True)
        else:
            print("telegram-cidrs-already-present", flush=True)
        command = (
            "/opt/bin/curl -sS --max-time 30 -X POST "
            "-w '\\nhttp=%{http_code}\\n' http://127.0.0.1:2222/api/mihomo/reload; sleep 4; "
            "grep -E '^(  - (IP-CIDR|GEOSITE).*(telegram|91\\.|95\\.161|149\\.154|185\\.76))' "
            "/opt/etc/awg-manager/mihomo/config.yaml 2>&1 || true"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "cold-restart-mihomo":
        command = (
            "for pid in $(pidof mihomo 2>/dev/null); do kill $pid; done; sleep 2; "
            "/opt/etc/init.d/S99awg-manager restart; sleep 7"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "start-mihomo":
        command = (
            "if pidof mihomo >/dev/null 2>&1; then echo already-running:$(pidof mihomo); "
            "else cd /opt/etc/awg-manager/mihomo && "
            "nohup ./mihomo -d /opt/etc/awg-manager/mihomo >/opt/var/log/mihomo-manual.log 2>&1 & "
            "echo started:$!; fi; sleep 4; "
            "netstat -lntup 2>/dev/null | grep -E ':(9090|51271|51272|1053)[[:space:]]' || "
            "tail -n 30 /opt/var/log/mihomo-manual.log"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "ports-only":
        command = "netstat -lntup 2>/dev/null | grep -E ':(9090|51271|51272|1053|1099)[[:space:]]' || true"
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "network-diag":
        checks = {
            "mangle_prerouting": "iptables -t mangle -L PREROUTING -n -v --line-numbers 2>&1 | grep -E 'Chain|AWGM'",
            "mangle_chain": "iptables -t mangle -L AWGM-TPROXY -n -v --line-numbers 2>&1",
            "nat_prerouting": "iptables -t nat -L PREROUTING -n -v --line-numbers 2>&1 | grep -E 'Chain|AWGM'",
            "nat_chain": "iptables -t nat -L AWGM-REDIRECT -n -v --line-numbers 2>&1",
            "route100": "ip route show table 100 2>&1",
            "mihomo_api": "wget -T 3 -qO- http://127.0.0.1:9090/version 2>&1",
            "ttus_delay": "wget -T 8 -qO- 'http://127.0.0.1:9090/proxies/ttUS/delay?url=http%3A%2F%2Fcp.cloudflare.com%2Fgenerate_204&timeout=5000' 2>&1",
            "awg_delay": "wget -T 8 -qO- 'http://127.0.0.1:9090/proxies/awg-awg20/delay?url=http%3A%2F%2Fcp.cloudflare.com%2Fgenerate_204&timeout=5000' 2>&1",
            "final_rules": "tail -n 12 /opt/etc/awg-manager/mihomo/config.yaml",
        }
        for label, command in checks.items():
            stdin, stdout, stderr = client.exec_command(command)
            output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
            print(f"--- {label} ---\n{output.strip()}", flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "api-diag":
        checks = {
			"version": "/opt/bin/curl -sS --max-time 5 -w '\nhttp=%{http_code} exit=%{exitcode}\n' http://127.0.0.1:9090/version 2>&1",
			"direct": "/opt/bin/curl -sS --max-time 10 -w '\nhttp=%{http_code} exit=%{exitcode}\n' 'http://127.0.0.1:9090/proxies/DIRECT/delay?url=http%3A%2F%2Fcp.cloudflare.com%2Fgenerate_204&timeout=5000' 2>&1",
			"ttus": "/opt/bin/curl -sS --max-time 10 -w '\nhttp=%{http_code} exit=%{exitcode}\n' 'http://127.0.0.1:9090/proxies/ttUS/delay?url=http%3A%2F%2Fcp.cloudflare.com%2Fgenerate_204&timeout=5000' 2>&1",
        }
        for label, command in checks.items():
            stdin, stdout, stderr = client.exec_command(command)
            output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
            print(f"--- {label} ---\n{output.strip()}", flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "mihomo-status":
        command = (
            "/opt/bin/curl -sS --max-time 5 -w '\\nhttp=%{http_code}\\n' "
            "http://127.0.0.1:2222/api/mihomo/status 2>&1"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "connectivity-diag":
        checks = {
            "router_gstatic": "/opt/bin/curl -sS --max-time 10 -o /dev/null -w 'http=%{http_code} ip=%{remote_ip} exit=%{exitcode}\\n' http://www.gstatic.com/generate_204 2>&1",
            "direct_gstatic": "/opt/bin/curl -sS --max-time 12 -w '\\nhttp=%{http_code} exit=%{exitcode}\\n' 'http://127.0.0.1:9090/proxies/DIRECT/delay?url=http%3A%2F%2Fwww.gstatic.com%2Fgenerate_204&timeout=8000' 2>&1",
            "ttus_gstatic": "/opt/bin/curl -sS --max-time 12 -w '\\nhttp=%{http_code} exit=%{exitcode}\\n' 'http://127.0.0.1:9090/proxies/ttUS/delay?url=http%3A%2F%2Fwww.gstatic.com%2Fgenerate_204&timeout=8000' 2>&1",
            "group": "/opt/bin/curl -sS --max-time 5 http://127.0.0.1:9090/proxies/Block 2>&1",
            "recent_mihomo": "logread 2>/dev/null | grep -i mihomo | tail -n 40",
        }
        for label, command in checks.items():
            stdin, stdout, stderr = client.exec_command(command)
            output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
            print(f"--- {label} ---\n{output.strip()}", flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "dot-diag":
        checks = {
            "google_853": "echo | timeout 6 nc 8.8.8.8 853 2>&1; echo exit=$?",
            "opendns_853": "echo | timeout 6 nc 208.67.222.222 853 2>&1; echo exit=$?",
        }
        for label, command in checks.items():
            stdin, stdout, stderr = client.exec_command(command)
            output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
            print(f"--- {label} ---\n{output.strip()}", flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "dns-diag":
        command = "if [ -x /opt/bin/dig ]; then /opt/bin/dig +time=3 +tries=1 @127.0.0.1 -p 1053 example.com A +short; else nslookup example.com 127.0.0.1:1053; fi 2>&1; echo exit=$?"
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "connections-diag":
        stdin, stdout, stderr = client.exec_command("/opt/bin/curl -sS --max-time 5 http://127.0.0.1:9090/connections")
        raw = stdout.read().decode("utf-8", "replace")
        err = stderr.read().decode("utf-8", "replace").strip()
        if err:
            print(err, flush=True)
        payload = json.loads(raw or "{}")
        summary = {}
        for connection in payload.get("connections") or []:
            meta = connection.get("metadata") or {}
            source = meta.get("sourceIP") or "unknown"
            key = (source, connection.get("rule") or "", tuple(connection.get("chains") or []))
            item = summary.setdefault(key, {"count": 0, "upload": 0, "download": 0})
            item["count"] += 1
            item["upload"] += int(connection.get("upload") or 0)
            item["download"] += int(connection.get("download") or 0)
        for (source, rule, chains), item in sorted(summary.items()):
            print(f"source={source} count={item['count']} upload={item['upload']} download={item['download']} rule={rule} chains={','.join(chains)}", flush=True)
        if not summary:
            print("no-active-connections", flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "phone-connections":
        stdin, stdout, stderr = client.exec_command("/opt/bin/curl -sS --max-time 5 http://127.0.0.1:9090/connections")
        payload = json.loads(stdout.read().decode("utf-8", "replace") or "{}")
        for connection in payload.get("connections") or []:
            meta = connection.get("metadata") or {}
            if meta.get("sourceIP") != "192.168.90.4":
                continue
            print(
                "network={network} host={host} dst={dst}:{port} type={type} rule={rule} chains={chains} up={up} down={down}".format(
                    network=meta.get("network") or "",
                    host=meta.get("host") or "",
                    dst=meta.get("destinationIP") or "",
                    port=meta.get("destinationPort") or "",
                    type=meta.get("type") or "",
                    rule=connection.get("rule") or "",
                    chains=",".join(connection.get("chains") or []),
                    up=connection.get("upload") or 0,
                    down=connection.get("download") or 0,
                ),
                flush=True,
            )
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "reset-phone-conntrack":
        command = (
            "ct=$(command -v conntrack 2>/dev/null || true); "
            "[ -n \"$ct\" ] || ct=/opt/sbin/conntrack; "
            "if [ -x \"$ct\" ]; then \"$ct\" -D -s 192.168.90.4 2>&1; "
            "else echo conntrack-not-installed; exit 3; fi"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "bypass-phone-dot":
        command = (
            "iptables -t nat -C AWGM-REDIRECT -p tcp --dport 853 -j RETURN 2>/dev/null || "
            "iptables -t nat -I AWGM-REDIRECT 19 -p tcp --dport 853 -j RETURN; "
            "ct=$(command -v conntrack 2>/dev/null || true); [ -n \"$ct\" ] || ct=/opt/sbin/conntrack; "
            "[ ! -x \"$ct\" ] || \"$ct\" -D -s 192.168.90.4 >/dev/null 2>&1 || true; "
            "iptables -t nat -L AWGM-REDIRECT -n --line-numbers | grep -E 'dpt:853|redir ports 51272'"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "routing-settings":
        command = (
            "/opt/bin/jq '{enabled:.singboxRouter.enabled,routingEngine:.singboxRouter.routingEngine,"
            "routingMode:.singboxRouter.routingMode,deviceMode:.singboxRouter.deviceMode,"
            "policyName:.singboxRouter.policyName,proxyGroups:.singboxRouter.proxyGroups}' "
            "/opt/etc/awg-manager/settings.json 2>/dev/null; "
            "echo ---router-slots---; "
            "for f in /opt/etc/awg-manager/singbox/config.d/20-router.json "
            "/opt/etc/awg-manager/singbox/config.d/disabled/20-router.json; do "
            "[ ! -f \"$f\" ] || { echo ===$f; /opt/bin/jq '{route:.route}' \"$f\" 2>/dev/null; }; done"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "policy-flow-diag":
        checks = {
            "table_4106": "ip route show table 4106 2>&1",
            "phone_conntrack": "conntrack -L -s 192.168.90.4 2>/dev/null | tail -n 40",
            "policy_arp": "ip neigh show dev br0 2>/dev/null | grep -E '192.168.90.[0-9]+' | tail -n 30",
            "chain_counters": "iptables -t nat -L AWGM-REDIRECT -n -v --line-numbers 2>&1 | grep -E 'Chain|dpt:853|redir ports 51272'",
        }
        for label, command in checks.items():
            stdin, stdout, stderr = client.exec_command(command)
            output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
            print(f"--- {label} ---\n{output.strip()}", flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "manager-diag":
        checks = {
            "package": "opkg status awg-manager | grep -E '^(Version|Status):'",
            "health": "/opt/bin/curl -sS --max-time 5 http://127.0.0.1:2222/api/health 2>&1",
            "pidof": "pidof awg-manager 2>&1 || true",
            "processes": "ps | grep '[a]wg-manager'",
            "port2222": "netstat -lntp 2>/dev/null | grep ':2222[[:space:]]' || true",
            "pidfiles": "find /opt/var/run /var/run -maxdepth 2 -type f -iname '*awg*' -exec sh -c 'echo ===$1; cat $1' sh {} \\; 2>/dev/null",
        }
        for label, command in checks.items():
            stdin, stdout, stderr = client.exec_command(command)
            output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
            print(f"--- {label} ---\n{output.strip()}", flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "lifecycle-diag":
        checks = {
            "processes": "ps | grep -E '[a]wg-manager|[m]ihomo|[s]ing-box'",
            "system_log": "logread 2>/dev/null | grep -Ei 'awg-manager|mihomo|reconcile|routing' | tail -n 120",
            "app_logs": "find /opt/var/log /opt/etc/awg-manager -maxdepth 3 -type f -iname '*log*' 2>/dev/null | head -n 30",
        }
        for label, command in checks.items():
            stdin, stdout, stderr = client.exec_command(command)
            output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
            print(f"--- {label} ---\n{output.strip()}", flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "rules-file-diag":
        command = (
            "for f in /opt/etc/awg-manager/singbox/router-netfilter*.rules; do "
            "echo ===$f; grep -E 'dport (53|853)|--dport (53|853)|51271|51272' $f 2>/dev/null; done"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "force-routing-reconcile":
        command = (
            "n=$(iptables -t mangle -L PREROUTING -n --line-numbers 2>/dev/null | awk '/AWGM-TPROXY/{print $1; exit}'); "
            "[ -z \"$n\" ] || iptables -t mangle -D PREROUTING $n; "
            "n=$(iptables -t nat -L PREROUTING -n --line-numbers 2>/dev/null | awk '/AWGM-REDIRECT/{print $1; exit}'); "
            "[ -z \"$n\" ] || iptables -t nat -D PREROUTING $n; "
            "/opt/etc/init.d/S99awg-manager restart; sleep 8"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "drop-routing-jumps":
        command = (
            "n=$(iptables -t mangle -L PREROUTING -n --line-numbers 2>/dev/null | awk '/AWGM-TPROXY/{print $1; exit}'); "
            "[ -z \"$n\" ] || iptables -t mangle -D PREROUTING $n; "
            "n=$(iptables -t nat -L PREROUTING -n --line-numbers 2>/dev/null | awk '/AWGM-REDIRECT/{print $1; exit}'); "
            "[ -z \"$n\" ] || iptables -t nat -D PREROUTING $n; "
            "echo routing-jumps-removed"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "restore-routing-jumps":
        command = (
            "while n=$(iptables -t mangle -L PREROUTING -n --line-numbers 2>/dev/null | awk '/AWGM-BLACKHOLE/{print $1; exit}'); "
            "[ -n \"$n\" ]; do iptables -t mangle -D PREROUTING $n; done; "
            "iptables -t mangle -C PREROUTING -m connmark --mark 0xffffaaf -m conntrack ! --ctstate INVALID -j AWGM-TPROXY 2>/dev/null || "
            "iptables -t mangle -A PREROUTING -m connmark --mark 0xffffaaf -m conntrack ! --ctstate INVALID -j AWGM-TPROXY; "
            "iptables -t nat -C PREROUTING -m connmark --mark 0xffffaaf -m conntrack ! --ctstate INVALID -j AWGM-REDIRECT 2>/dev/null || "
            "iptables -t nat -A PREROUTING -m connmark --mark 0xffffaaf -m conntrack ! --ctstate INVALID -j AWGM-REDIRECT; "
            "echo routing-jumps-restored"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "park-deviceproxy":
        command = (
            "base=/opt/etc/awg-manager/singbox/config.d; src=$base/30-deviceproxy.json; "
            "dst=$base/disabled/30-deviceproxy.json; mkdir -p $base/disabled; "
            "if [ -f $src ] && [ ! -e $dst ]; then mv $src $dst; echo parked; "
            "elif [ ! -e $src ] && [ -f $dst ]; then echo already-parked; "
            "else echo ambiguous-slot-state; exit 2; fi; "
            "pid=$(pidof sing-box 2>/dev/null || true); [ -z \"$pid\" ] || kill -HUP $pid; sleep 3"
        )
        stdin, stdout, stderr = client.exec_command(command)
        output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
        print(output.strip(), flush=True)
        client.close()
        return

    if len(sys.argv) > 1 and sys.argv[1] == "verify":
        checks = {
            "package": "opkg status awg-manager | grep -E '^(Package|Version|Status):'",
            "service": "/opt/etc/init.d/S99awg-manager status 2>&1",
            "health": "wget -qO- http://127.0.0.1:2222/api/health 2>&1",
            "config_test": "/opt/etc/awg-manager/mihomo/mihomo -d /opt/etc/awg-manager/mihomo -t 2>&1",
            "mihomo_process": "ps | grep '[m]ihomo'",
			"singbox_process": "ps | grep '[s]ing-box'",
			"ports": "netstat -lntup 2>/dev/null | grep -E ':(9090|51271|51272|1053|1099)[[:space:]]' || true",
			"mangle_rules": "iptables -t mangle -L AWGM-TPROXY -n -v --line-numbers 2>&1 | tail -n 20",
			"nat_rules": "iptables -t nat -L AWGM-REDIRECT -n -v --line-numbers 2>&1 | tail -n 20",
			"policy_route": "ip rule show; ip route show table 100 2>&1",
			"active_slots": "find /opt/etc/awg-manager/sing-box -maxdepth 2 -type f -name '*.json' 2>/dev/null | sed 's#^.*/sing-box/##' | sort",
            "awg_proxy": "grep -A4 -B1 'name: awg-awg20' /opt/etc/awg-manager/mihomo/config.yaml 2>/dev/null || true",
			"awg_slot_fields": "find /opt/etc/awg-manager -name 15-awg.json -exec grep -Eo '\"(tag|bind_interface)\"[[:space:]]*:[[:space:]]*\"[^\"]+\"' {} \\; 2>/dev/null",
			"ttus_slot": "find /opt/etc/awg-manager -type f -exec grep -l '\"ttUS\"' {} \\; 2>/dev/null",
			"mihomo_group_types": "sed -n '/^proxy-groups:/,/^[a-z][a-z-]*:/p' /opt/etc/awg-manager/mihomo/config.yaml | grep -E '^[[:space:]]*(- name:|type:)'",
        }
        for label, command in checks.items():
            stdin, stdout, stderr = client.exec_command(command)
            output = stdout.read().decode("utf-8", "replace") + stderr.read().decode("utf-8", "replace")
            print(f"--- {label} ---\n{output.strip()}", flush=True)
        client.close()
        return

    # Use the most recently built IPK
    ipks = glob.glob(f"dist/*{entware_arch}*.ipk")
    if not ipks:
        print(f"No IPK found for {entware_arch}! Please build first.", flush=True)
        sys.exit(1)
    
    ipk_path = max(ipks, key=os.path.getctime)
    ipk_filename = os.path.basename(ipk_path)
    print(f"Found IPK: {ipk_path}", flush=True)

    remote_path = f"/opt/tmp/{ipk_filename}"
    print(f"Uploading to {remote_path} via stream...", flush=True)
    stdin, stdout, stderr = client.exec_command(f"cat > {remote_path}")
    with open(ipk_path, "rb") as f:
        stdin.write(f.read())
    stdin.channel.shutdown_write()
    stdout.read() # wait for exit

    print("Installing package with approved safe method (--force-downgrade --force-overwrite)...", flush=True)
    stdin, stdout, stderr = client.exec_command(f"opkg install --force-downgrade --force-overwrite {remote_path}")
    print(stdout.read().decode("utf-8", "replace"), flush=True)
    print(stderr.read().decode("utf-8", "replace"), flush=True)

    print("Deploy complete!", flush=True)
    client.close()

if __name__ == "__main__":
    deploy()
