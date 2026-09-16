# -*- coding: utf-8 -*-
import requests
import re

s = requests.Session()
s.post("http://192.168.90.1:2222/api/auth/login", json={"login": "root", "password": "Qaz74251106"})

r = s.get("http://192.168.90.1:2222/servers")
print("GET /servers status:", r.status_code, "length:", len(r.text))

chunks = re.findall(r'src="([^"]+\.js)"', r.text)
print("Found JS chunks:", len(chunks))

found_xray = False
found_tg = False
for chunk in chunks:
    url = "http://192.168.90.1:2222" + chunk
    res = s.get(url)
    if "Xray VLESS" in res.text or "xray" in res.text:
        found_xray = True
    if "Telegram" in res.text or "telemt" in res.text or "tgwebproxy" in res.text:
        found_tg = True

print(f"Frontend checks: found_xray={found_xray}, found_tg={found_tg}")
