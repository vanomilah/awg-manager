import os
import subprocess

print("Building frontend...")
subprocess.run(["wsl", "-d", "Ubuntu", "bash", "-lc", "cd /mnt/e/AWGM/awg-manager/frontend && npm run build && npm run postbuild"], check=True)

print("Building IPK...")
subprocess.run(["wsl", "-d", "Ubuntu", "bash", "-lc", "cd /mnt/e/AWGM/awg-manager && ./scripts/build-ipk.sh aarch64-3.10"], check=True)
