import paramiko

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect("192.168.90.1", 22, "root", "Qaz74251106")

cmd = """
for f in /opt/var/run/*.pid; do
    [ -f "$f" ] || continue
    pid=$(cat "$f")
    echo "File: $f PID: $pid"
    if [ -d "/proc/$pid" ]; then
        echo "  exe: $(ls -l /proc/$pid/exe 2>/dev/null)"
        echo "  cmdline: $(tr '\\0' ' ' < /proc/$pid/cmdline 2>/dev/null)"
        echo "  stat field 22: $(cut -d' ' -f22 /proc/$pid/stat 2>/dev/null)"
    else
        echo "  /proc/$pid does not exist!"
    fi
done
"""
stdin, stdout, stderr = client.exec_command(cmd)
print("STDOUT:\n" + stdout.read().decode())
print("STDERR:\n" + stderr.read().decode())
client.close()
