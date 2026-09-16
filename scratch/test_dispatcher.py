import socket

# Test dispatch to Xray (/cdn-bridge/)
s1 = socket.create_connection(("192.168.90.1", 9009), timeout=3)
req1 = b"GET /cdn-bridge/ HTTP/1.1\r\nHost: cdn.vinvanvladnet.ru\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n"
s1.sendall(req1)
resp1 = s1.recv(1024)
print("CDN /cdn-bridge/ (Xray) response:", resp1[:100])
s1.close()

# Test dispatch to Telegram Web Proxy (/tproxy/ or secret)
s2 = socket.create_connection(("192.168.90.1", 9009), timeout=3)
req2 = b"GET /f3edd6341a1f278031e2a7d913078cde HTTP/1.1\r\nHost: cdn.vinvanvladnet.ru\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n"
s2.sendall(req2)
resp2 = s2.recv(1024)
print("CDN /f3edd... (TgWebProxy) response:", resp2[:100])
s2.close()
