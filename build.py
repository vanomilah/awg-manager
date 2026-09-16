import os
import subprocess

print("Building frontend...")
subprocess.run("npm run build", cwd="frontend", shell=True, check=True)
subprocess.run("npm run postbuild", cwd="frontend", shell=True, check=True)

print("Building IPK...")
subprocess.run(["wsl", "-d", "Ubuntu", "bash", "-lc", "cd /mnt/e/AWGM/awg-manager && ./scripts/build-ipk.sh aarch64-3.10"], check=True)
