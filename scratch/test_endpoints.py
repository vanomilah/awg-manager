import requests

base = "http://192.168.90.1:2222"
endpoints = [
    "/api/health",
    "/api/servers/xray/status",
    "/api/servers/tgwebproxy/status",
    "/api/servers/xray",
    "/api/servers/tgwebproxy",
    "/api/mihomo/status",
    "/api/ai/status",
    "/api/ai/history",
    "/api/proxy/instances"
]

for ep in endpoints:
    url = base + ep
    try:
        r = requests.get(url, timeout=5)
        print(f"{ep}: status={r.status_code}, data={r.text[:120]}")
    except Exception as e:
        print(f"{ep}: error={e}")
