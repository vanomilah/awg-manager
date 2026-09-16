import paramiko

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect("192.168.90.1", 22, "root", "Qaz74251106")

stdin, stdout, stderr = client.exec_command("netstat -lntp 2>/dev/null | grep -E ':(8443|2398|8085|8086|1099|2222)[[:space:]]'")
print("PORTS:\n" + stdout.read().decode())
stdin, stdout, stderr = client.exec_command("/opt/bin/curl -sS http://127.0.0.1:2222/api/servers/tgwebproxy/status")
print("TGWEBPROXY STATUS:\n" + stdout.read().decode())
client.close()
