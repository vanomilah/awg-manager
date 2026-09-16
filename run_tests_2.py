import subprocess
import os
import sys

os.environ['GOOS'] = 'linux'
# No need to compile again, we have mihomo.test
result = subprocess.run(['wsl', './mihomo.test'], cwd=r'e:\AWGM\awg-manager', capture_output=True, text=True)
print(result.stdout)
print(result.stderr)
