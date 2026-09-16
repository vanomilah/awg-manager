import paramiko
import json

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect("192.168.90.1", 22, "root", "Qaz74251106")

cmd = """
COOKIE_JAR=/tmp/awgm_cookie.txt
/opt/bin/curl -sS -c $COOKIE_JAR -X POST -H "Content-Type: application/json" -d '{"login":"root","password":"Qaz74251106"}' http://127.0.0.1:2222/api/auth/login >/dev/null

echo "=== CALLING ACTION: START ==="
/opt/bin/curl -sS -b $COOKIE_JAR -X POST -H "Content-Type: application/json" -H "X-CSRF-Token: awgm-tgwebproxy" -d '{"action":"start"}' http://127.0.0.1:2222/api/servers/tgwebproxy/action
echo ""

echo "=== VERIFYING PROCESSES ==="
ps | grep -E 'telemt|tproxy-server' | grep -v grep

echo "=== VERIFYING LISTENING PORTS ==="
netstat -lntp 2>/dev/null | grep -E ':(8443|2398|8085|8086)[[:space:]]'

rm -f $COOKIE_JAR
"""

stdin, stdout, stderr = client.exec_command(cmd)
print(stdout.read().decode())
client.close()
