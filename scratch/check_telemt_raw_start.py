import paramiko

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect("192.168.90.1", 22, "root", "Qaz74251106")

cmd = """
echo "=== EXECUTING S96telemt-raw start ==="
/opt/etc/init.d/S96telemt-raw start
echo "exit code: $?"

echo "=== CHECKING PID FILE ==="
ls -l /opt/var/run/telemt-raw*
cat /opt/var/run/telemt-raw.pid 2>/dev/null; echo ""

echo "=== CHECKING LOG FILE ==="
tail -n 30 /opt/var/log/telemt-raw.log 2>/dev/null || true

echo "=== DIRECT EXECUTION TEST ==="
telemt start /opt/etc/telemt/raw.toml --pid-file /opt/var/run/telemt-raw.pid 2>&1
"""

stdin, stdout, stderr = client.exec_command(cmd)
print(stdout.read().decode())
client.close()
