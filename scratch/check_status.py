import paramiko
c = paramiko.SSHClient()
c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect("192.168.90.1", 22, "root", "Qaz74251106")
cmd = """
opkg status awg-manager
echo "--- service ---"
/opt/etc/init.d/S99awg-manager status
echo "--- health ---"
/opt/bin/curl -s http://127.0.0.1:2222/api/health
echo ""
echo "--- processes ---"
ps | grep -E "awg-manager|xray|tproxy|telemt|sing-box|mihomo" | grep -v grep
echo "--- netstat ---"
netstat -lntp 2>/dev/null | grep -E ":(9009|2222|9090|8045|51271)"
"""
stdin, stdout, stderr = c.exec_command(cmd)
print(stdout.read().decode("utf-8", "replace"))
c.close()
