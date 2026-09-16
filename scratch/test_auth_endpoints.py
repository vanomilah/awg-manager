import requests

s = requests.Session()
login_res = s.post("http://192.168.90.1:2222/api/auth/login", json={"login": "root", "password": "Qaz74251106"})
print("Login status:", login_res.status_code, login_res.text)

endpoints = [
    "/api/servers/xray/status",
    "/api/servers/tgwebproxy/status",
    "/api/servers/xray",
    "/api/servers/tgwebproxy",
    "/api/ai/status",
    "/api/proxy/instances"
]

for ep in endpoints:
    url = "http://192.168.90.1:2222" + ep
    r = s.get(url, timeout=5)
    print(f"{ep}: status={r.status_code}")
    print(f"  {r.text[:150]}")
