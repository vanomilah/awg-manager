import paramiko

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect("192.168.90.1", 22, "root", "Qaz74251106")

cmd = """
COOKIE_JAR=/tmp/awgm_cookie.txt
/opt/bin/curl -sS -c $COOKIE_JAR -X POST -H "Content-Type: application/json" -d '{"login":"root","password":"Qaz74251106"}' http://127.0.0.1:2222/api/auth/login >/dev/null
echo "=== FIRST REVEAL ==="
/opt/bin/curl -sS -w "\nHTTP_CODE: %{http_code}\n" -b $COOKIE_JAR -X POST -H "X-CSRF-Token: awgm-tgwebproxy" http://127.0.0.1:2222/api/servers/tgwebproxy/reveal
echo "=== SECOND IMMEDIATE REVEAL (EXPECT 429) ==="
/opt/bin/curl -sS -w "\nHTTP_CODE: %{http_code}\n" -b $COOKIE_JAR -X POST -H "X-CSRF-Token: awgm-tgwebproxy" http://127.0.0.1:2222/api/servers/tgwebproxy/reveal
echo "=== ACTION WITHOUT CSRF (EXPECT 403) ==="
/opt/bin/curl -sS -w "\nHTTP_CODE: %{http_code}\n" -b $COOKIE_JAR -X POST -d '{"action":"restart"}' http://127.0.0.1:2222/api/servers/tgwebproxy/action
rm -f $COOKIE_JAR
"""

stdin, stdout, stderr = client.exec_command(cmd)
print(stdout.read().decode())
client.close()
