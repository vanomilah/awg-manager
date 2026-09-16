import subprocess
import os

os.environ['GOOS'] = 'linux'
subprocess.run(['go', 'test', '-c', './internal/mihomo'], cwd=r'e:\AWGM\awg-manager', check=True)
print("Compiled.")

result = subprocess.run(['wsl', './mihomo.test'], cwd=r'e:\AWGM\awg-manager', capture_output=True, text=True)
print("Return code:", result.returncode)

for line in result.stdout.split('\n'):
    if 'FAIL' in line or '---' in line or 'panic' in line:
        print(line)

for line in result.stderr.split('\n'):
    if line.strip():
        print("ERR:", line)
