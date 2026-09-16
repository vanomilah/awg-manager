import paramiko

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect("192.168.90.1", 22, "root", "Qaz74251106")

cmd = """
echo "=== PROCESSES ==="
ps | grep -E 'telemt|tproxy|awg-manager|mihomo' | grep -v grep

echo "=== LISTENING PORTS ==="
netstat -lntp 2>/dev/null | grep -E ':(8443|2398|8085|8086|8080|1099|2222)[[:space:]]'

echo "=== PID FILES ==="
ls -l /opt/var/run/*telemt* /opt/var/run/*tproxy* 2>/dev/null || true
"""
stdin, stdout, stderr = client.exec_command(cmd)
print(stdout.read().decode())
client.close()
