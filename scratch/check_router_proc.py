import paramiko

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect("192.168.90.1", 22, "root", "Qaz74251106")

stdin, stdout, stderr = client.exec_command('cut -d" " -f22 /proc/1/stat; date +%s; date -u +%s')
print("STDOUT:", stdout.read().decode())
print("STDERR:", stderr.read().decode())
client.close()
