import paramiko

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect('192.168.90.1', 22, 'root', 'Qaz74251106', timeout=10)
stdin, stdout, stderr = client.exec_command("netstat -lntup | grep -E ':(2222|1099|9090|8443|51271|51272|1053) '")
print(stdout.read().decode())
stdin, stdout, stderr = client.exec_command("ndmc -c 'show interface Wireguard2'")
print(stdout.read().decode())
client.close()
