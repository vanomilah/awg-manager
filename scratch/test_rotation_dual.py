import paramiko
import json

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect("192.168.90.1", 22, "root", "Qaz74251106")

cmd = """
COOKIE_JAR=/tmp/awgm_cookie.txt
/opt/bin/curl -sS -c $COOKIE_JAR -X POST -H "Content-Type: application/json" -d '{"login":"root","password":"Qaz74251106"}' http://127.0.0.1:2222/api/auth/login >/dev/null

echo "=== METRICS TEST ==="
/opt/bin/curl -sS http://127.0.0.1:8086/metrics | grep tproxy_

echo "=== ROTATE SECRET ACTION ==="
/opt/bin/curl -sS -b $COOKIE_JAR -X POST -H "Content-Type: application/json" -H "X-CSRF-Token: awgm-tgwebproxy" -d '{"action":"rotate_secret"}' http://127.0.0.1:2222/api/servers/tgwebproxy/action
echo ""

echo "=== CHECKING PROFILES.JSON FOR DUAL PROFILES ==="
cat /opt/etc/awg-manager/tproxy/profiles.json

echo "=== CHECKING TELEMT CONFIG.TOML FOR DUAL USERS ==="
grep -A3 '\[access.users\]' /opt/etc/telemt/config.toml

echo "=== CHECKING TELEMT RAW.TOML FOR DUAL USERS ==="
grep -A3 '\[access.users\]' /opt/etc/telemt/raw.toml

echo "=== CHECKING WORKER PROCESSES ==="
ps | grep -E 'telemt|tproxy-server' | grep -v grep

rm -f $COOKIE_JAR
"""

stdin, stdout, stderr = client.exec_command(cmd)
print(stdout.read().decode())
client.close()
