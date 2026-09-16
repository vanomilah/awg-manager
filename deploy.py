import sys
import paramiko
import os
import glob

def deploy():
    host = "192.168.90.60"
    port = 222
    user = "root"
    password = "keenetic"

    client = paramiko.SSHClient()
    client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    
    print("Connecting to router...")
    client.connect(host, port=port, username=user, password=password)

    stdin, stdout, stderr = client.exec_command("uname -m")
    arch = stdout.read().decode().strip()
    print(f"Router architecture: {arch}")

    # Determine entware arch
    entware_arch = ""
    if arch == "aarch64":
        entware_arch = "aarch64-3.10"
    elif "mips" in arch:
        entware_arch = "mipsel-3.4"
    else:
        entware_arch = "mipsel-3.4" # fallback
    
    print(f"Target Entware arch: {entware_arch}")

    # Clean old IPKs
    print("Cleaning old IPKs...")
    for old_ipk in glob.glob(f"dist/*{entware_arch}*.ipk"):
        os.remove(old_ipk)

    # Build via WSL
    print("Building package via WSL...")
    os.system(f'wsl -d Ubuntu bash -lc "cd /mnt/e/AWGM/awg-manager && ./scripts/build-ipk.sh {entware_arch}"')

    # Find built IPK
    ipks = glob.glob(f"dist/*{entware_arch}*.ipk")
    if not ipks:
        print("Build failed, no IPK found.")
        sys.exit(1)
    
    ipk_path = ipks[0]
    ipk_filename = os.path.basename(ipk_path)
    print(f"Found IPK: {ipk_path}")

    # Upload via stdin
    remote_path = f"/opt/tmp/{ipk_filename}"
    print(f"Uploading to {remote_path} via stream...")
    stdin, stdout, stderr = client.exec_command(f"cat > {remote_path}")
    with open(ipk_path, "rb") as f:
        stdin.write(f.read())
    stdin.channel.shutdown_write()
    stdout.read() # wait for exit

    # Install
    print("Installing WITHOUT --force-reinstall (using --force-downgrade)...")
    stdin, stdout, stderr = client.exec_command(f"opkg install --force-downgrade {remote_path}")
    print(stdout.read().decode())
    print(stderr.read().decode())

    print("Deploy complete!")
    client.close()

if __name__ == "__main__":
    deploy()
