# Отчёт о деплое awg-manager v2.17.49 на роутер Дача (192.168.50.1)

**Дата и время:** 17 сентября 2026 г., 11:07 MSK  
**Целевое устройство:** Keenetic Hopper 4G+ (KN-1812 / NC-1812), KeeneticOS 5.01, aarch64  
**IP-адрес:** `192.168.50.1`  
**Пакет:** `awg-manager_2.17.49_aarch64-3.10-kn.ipk` (11.5 МБ)  
**Способ развёртывания:** `opkg install --force-downgrade --force-overwrite` (согласно GEMINI.md, без `--force-reinstall`)

---

## 1. Резюме развёртывания

| Параметр / Проверка | Значение | Результат |
|---------------------|----------|-----------|
| **Версия бинарника** | `awg-manager version 2.17.49` | **PASS** |
| **Статус opkg** | `Package: awg-manager, Version: 2.17.49, Status: install user installed` | **PASS** |
| **Движок маршрутизации** | `mihomo` (tproxy mode) | **PASS** |
| **Процесс awg-manager** | `/opt/bin/awg-manager -data-dir /opt/etc/awg-manager` (PID 3233) | **PASS** |
| **Процесс mihomo** | `/opt/etc/awg-manager/mihomo/mihomo -d /opt/etc/awg-manager/mihomo` (PID 3545) | **PASS** |
| **Зомби-процессы `<defunct>`** | 0 (отсутствуют, исправлено в v2.17.49) | **PASS** |
| **Mihomo Clash API (порт 9090)** | `v1.19.29`, 64 прокси/аутбаунда загружено | **PASS** |
| **Web UI (порт 2222)** | `HTTP/1.1 200 OK` (проверено локально и с ПК `192.168.90.50`) | **PASS** |
| **Порт TProxy UDP (51271)** | `LISTEN` (mihomo PID 3545) | **PASS** |
| **Порт Redirect TCP (51272)** | `LISTEN` (mihomo PID 3545) | **PASS** |
| **Netfilter AWGM-TPROXY** | Правила активны, DNS и локальные сети исключены, TProxy активен | **PASS** |
| **Netfilter AWGM-REDIRECT** | Правила активны, зафиксирован перехват 21 пакета (1260 байт) на порт 51272 | **PASS** |
| **Резервная копия конфигов** | `/opt/tmp/pre_upgrade_backup_2.17.49/` | **СОЗДАНА** |
| **Очистка временных файлов** | Временный IPK в `/opt/tmp/` удалён | **PASS** |

---

## 2. Детали проверки служб

### Версия и статус opkg
```text
awg-manager version 2.17.49
Package: awg-manager
Version: 2.17.49
Status: install user installed
```

### Активные процессы
```text
3233 root 1321m S  /opt/bin/awg-manager -data-dir /opt/etc/awg-manager
3545 root 1476m S  /opt/etc/awg-manager/mihomo/mihomo -d /opt/etc/awg-manager/mihomo
19967 root 1290m S  /opt/etc/awg-manager/singbox/sing-box run -C /opt/etc/awg-manager/singbox/config.d
```
*Зомби `[mihomo] <defunct>` полностью устранены.*

### Порты и контроллер Mihomo
- Clash REST API: `http://127.0.0.1:9090/version` -> `{"meta":true,"version":"v1.19.29"}`
- Число прокси в пуле Mihomo: `64`
- Веб-интерфейс: `http://192.168.50.1:2222/` -> `HTTP/1.1 200 OK`

### Таблица iptables (активность)
- Цепочка `AWGM-REDIRECT`: зафиксирован трафик на порт `51272` (21 пакет, 1260 байт).
- Цепочка `AWGM-TPROXY`: перенаправление UDP на `51271`.

---

## 3. Вывод
Деплой релиза **awg-manager v2.17.49** на роутер Дача (`192.168.50.1`) успешно завершён. Все сервисы функционируют штатно, зомби-процессы отсутствуют, маршрутизация Mihomo активна.
