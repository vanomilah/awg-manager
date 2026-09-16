# wdtt-server из APK qWDTT 1.4

## Извлечение

```bash
bash scripts/extract-wdtt-from-apk.sh
# → build/wdtt/wdtt-server-linux-amd64-qwdtt-1.4.0
```

APK: [app-universal-release.apk](https://github.com/SpaceNeuroX/proxy-turn-vk-android/releases/download/v1.4.0/app-universal-release.apk), файл `assets/server`.

## Что внутри (проверено)

| Свойство | Значение |
|----------|----------|
| Формат | ELF 64-bit **x86-64**, static, Go 1.26 |
| Модуль | `wg-turn-client`, commit `2dd5d37` (**+dirty** — есть код не из GitHub) |
| Флаги | `-listen`, `-listen-raw`, `-listen-direct`, `-dns`, `-wg-port`, … |
| Нет | `-no-nat`, `-wg-iface` (патчи Keenetic / awg-manager) |

## Ограничения

1. **Только amd64** — APK кладёт сервер для **VPS deploy** (SSH), не для роутера arm64.
2. **Raw есть в бинарнике**, но **исходники с `-listen-raw` в GitHub на v1.4.0 не опубликованы** (`vcs.modified=true` в `go version -m`).
## Клиент Linux

В APK нет linux-бинарника — только `lib/arm64-v8a/libclient.so` (Go 1.26, commit `2dd5d37+dirty`).

RE-анализ: `python3 scripts/analyze-libclient-re.py` → `build/wdtt-apk/libclient_re_report.txt`

| APK (`libclient.so`) | Наш `wt-client` (Keenetic) |
|------------------------|----------------------------|
| TUN fd от Android (`recvTunFD`, `-tun-fd-sock`) | `tun.CreateTUN(-tun-name opkgtunN)` + virtio hdr |
| `NewDispatcherPendingTUN` + `AttachTUN` | `pendingPacketConn` + attach после RAWCONF |
| `RunSession` + `WorkerGroup` (единый код) | `RunRawSession` + `WorkerGroupRaw` (форк) |
| `LimitedAsyncPacketPipe(N)` | `AsyncPacketPipe()` без лимита |
| `chunkSizeFor(nWorkers)` | upstream `chunkSize=8` |
| Plain IP на TUN fd | wireguard-go tun.Device |

`wt-client` для Keenetic: `go_client/` v1.4.0 + `contrib/wdtt-client-patch` (без dispatcher.go).

## Keenetic (arm64)

| Задача | Решение |
|--------|---------|
| Клиент Raw → VPS qWDTT | Собрать `wt-client` 1.4, peer `:56003` |
| Сервер Raw **на роутере** | **`bash scripts/build-wdtt-server.sh`** → arm64 с `-listen-raw` |
| Сервер Raw **на VPS** | Бинарник из APK / деплой qWDTT |

## Эталон SHA256 (qWDTT 1.4.0 universal)

```
SHA256=cdaaf1d0e40c249a372994958df53fe1354f5b33cb3c454a9a8263b9f8eb655e
Size=8835234
```

Не коммитьте 8+ МБ бинарник в git без необходимости — достаточно скрипта извлечения и зеркала `repo.hoaxisr.ru`.
