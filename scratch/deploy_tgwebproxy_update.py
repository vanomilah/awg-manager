import paramiko
import os
import sys
import time

def upload_file(client, local_path, remote_path):
    print(f"Uploading {local_path} to {remote_path}...", flush=True)
    stdin, stdout, stderr = client.exec_command(f"cat > {remote_path}")
    with open(local_path, "rb") as f:
        while True:
            chunk = f.read(65536)
            if not chunk:
                break
            stdin.write(chunk)
    stdin.channel.shutdown_write()
    stdout.read() # wait for command completion
    exit_status = stdout.channel.recv_exit_status()
    if exit_status != 0:
        err = stderr.read().decode()
        raise RuntimeError(f"Upload to {remote_path} failed: {err}")

def deploy():
    host = "192.168.90.1"
    port = 22
    user = "root"
    password = "Qaz74251106"

    client = paramiko.SSHClient()
    client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    print("Connecting to router...", flush=True)
    client.connect(host, port=port, username=user, password=password)

    # 1. Check health
    stdin, stdout, stderr = client.exec_command("/opt/bin/curl -sS --max-time 5 http://127.0.0.1:2222/api/health")
    health = stdout.read().decode().strip()
    print(f"Pre-deploy health: {health}", flush=True)

    # 2. Backup current awg-manager binary
    client.exec_command("[ -f /opt/bin/awg-manager.bak.pre-tgwebproxy ] || cp /opt/bin/awg-manager /opt/bin/awg-manager.bak.pre-tgwebproxy")

    # 3. Upload init scripts
    print("Uploading init scripts from entware/files/etc/init.d...", flush=True)
    upload_file(client, "entware/files/etc/init.d/S95tproxy-server", "/opt/etc/init.d/S95tproxy-server")
    upload_file(client, "entware/files/etc/init.d/S99telemt", "/opt/etc/init.d/S99telemt")
    upload_file(client, "entware/files/etc/init.d/S96telemt-raw", "/opt/etc/init.d/S96telemt-raw")
    client.exec_command("chmod 755 /opt/etc/init.d/S95tproxy-server /opt/etc/init.d/S99telemt /opt/etc/init.d/S96telemt-raw")

    # 4. Restart workers with hardened scripts
    print("Restarting workers via init.d...", flush=True)
    for s in ["S96telemt-raw", "S99telemt", "S95tproxy-server"]:
        stdin, stdout, stderr = client.exec_command(f"/opt/etc/init.d/{s} restart")
        print(f"{s} restart: {stdout.read().decode().strip()} {stderr.read().decode().strip()}")
        time.sleep(1)

    # 6. Upload new awg-manager-arm64
    local_bin = "awg-manager-arm64"
    local_size = os.path.getsize(local_bin)
    print(f"Uploading {local_bin} ({local_size} bytes)...", flush=True)
    upload_file(client, local_bin, "/opt/bin/awg-manager.new")

    # Verify size
    stdin, stdout, stderr = client.exec_command("ls -l /opt/bin/awg-manager.new")
    res = stdout.read().decode().strip()
    print(f"Remote file: {res}", flush=True)

    # 7. Atomic rename and restart
    client.exec_command("chmod 755 /opt/bin/awg-manager.new && mv /opt/bin/awg-manager.new /opt/bin/awg-manager")
    print("Restarting S99awg-manager...", flush=True)
    stdin, stdout, stderr = client.exec_command("/opt/etc/init.d/S99awg-manager restart; sleep 4")
    print(stdout.read().decode().strip())

    # 8. Post-deploy verification
    print("Verifying post-deploy state...", flush=True)
    time.sleep(2)
    stdin, stdout, stderr = client.exec_command("/opt/bin/curl -sS --max-time 5 http://127.0.0.1:2222/api/health")
    post_health = stdout.read().decode().strip()
    print(f"Post-deploy health: {post_health}", flush=True)

    stdin, stdout, stderr = client.exec_command("""
echo "=== LISTENING PORTS ==="
netstat -lntp 2>/dev/null | grep -E ':(8443|2398|8085|8086|1099|2222)[[:space:]]'
echo "=== WORKER PIDS ==="
ls -l /opt/var/run/*telemt* /opt/var/run/*tproxy*
echo "=== PROCESSES ==="
ps | grep -E 'telemt|tproxy-server|awg-manager' | grep -v grep
""")
    print(stdout.read().decode())

    client.close()
    print("Deployment completed successfully!", flush=True)

if __name__ == "__main__":
    deploy()
