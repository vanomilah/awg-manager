import paramiko

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect("192.168.90.1", 22, "root", "Qaz74251106")

cmd = """
sed -i 's/log_level = "info"/log_level = "normal"/' /opt/etc/telemt/raw.toml /opt/etc/telemt/config.toml
/opt/etc/init.d/S96telemt-raw restart
sleep 1
/opt/etc/init.d/S96telemt-raw status
netstat -lntp 2>/dev/null | grep 2398
"""

stdin, stdout, stderr = client.exec_command(cmd)
print(stdout.read().decode())
client.close()
