# Отчёт о проделанной работе: Исправление адаптивной маршрутизации Susanin (Mihomo Egress)

**Дата:** 23 сентября 2026 г.  
**Целевое устройство:** Домашний интернет-центр Keenetic (IP: `192.168.90.1`)  
**Тестируемое клиентское устройство:** Телефон `HONOR-View20-Я` (IP: `192.168.90.4`, MAC: `34:29:12:9f:26:7a`, Политика: `Policy4 (Susanin)`)  
**Ветка проекта:** `feature/mihomo-ai-proxyrt` в `e:\AWGM\awg-manager`  
**Итоговая версия пакета:** `2.17.68` (`awg-manager_2.17.68_aarch64-3.10-kn.ipk`)

---

## 1. Контекст задачи и ограничения

### Цель
Восстановить работоспособность адаптивной маршрутизации Susanin через Mihomo TUN (`awgsus0`, egress: `🇫🇮 FIN (Финляндия) (Premium)`), чтобы у устройств в политике `Policy4` (в частности, телефона пользователя `192.168.90.4`) открывался Telegram и заблокированные веб-ресурсы без сбоев.

### Соблюдение строгих системных правил
1. **Абсолютный запрет на `--force-reinstall`:**  
   Ни при каких условиях команда `opkg install --force-reinstall` не использовалась. Все развёртывания производились строго по утверждённой процедуре:  
   `opkg install --force-downgrade --force-overwrite <package.ipk>`
2. **Абсолютный запрет на `--cleanup`:**  
   Скрипты очистки и удаление рабочих туннелей не выполнялись.
3. **Неприкосновенность инфраструктуры:**
   - Рабочий каталог строго `e:\AWGM\awg-manager` (директория `awg-manager-mihomo` не затрагивалась).
   - Порт `1099` (Mihomo mixed-port) сохранён активным.
   - Порт `2222` (веб-интерфейс AWG Manager) сохранён активным.
   - Сервис `telemt` на `0.0.0.0:8443` сохранён активным.
   - Интерфейс `Wireguard2` (порт 51820, пиры `FreeTurnDacha` и `Dacha`) остался в рабочем состоянии.

---

## 2. Диагностика и выявленные проблемы (Root Cause Analysis)

### 2.1. Коллизия меток трафика с политиками Keenetic
* **Диагностика:**  
  В правилах PREROUTING роутера Keenetic встроенная цепочка `_NDM_HOTSPOT_PREROUTING_MANGL` сопоставляет MAC-адрес телефона `34:29:12:9F:26:7A` и выставляет марку:
  ```
  -A _NDM_HOTSPOT_PREROUTING_MANGL -m mac --mac-source 34:29:12:9F:26:7A -j MARK --set-xmark 0xffffab7/0xffffffff
  ```
  Младшие 28 бит заняты меткой политики `Policy4` (`0x0ffffab7`).
* **Дефект:**  
  Оригинальный скрипт `datapath.sh` сопоставлял только пакеты без марки (`-m mark --mark 0x0/0xffffffff`). Пакеты с телефона уже имели ненулевую марку Keenetic, поэтому они **никогда не попадали в правила Susanin**.
* **Решение:**  
  Введена маска `0x30000000` (биты 28 и 29).  
  - Сопоставление: `-m mark --mark 0x0/0x30000000` игнорирует метку политики в младших битах.
  - Установка марки: `--set-xmark 0x20000000/0x30000000` выставляет только старшие биты, сохраняя марку Keenetic неизменной (`0x0ffffab7` -> `0x2ffffab7`).
  - Правила `ip rule 95/96` перенаправлены на проверку битовой маски: `fwmark 0x20000000/0x30000000 lookup 105`.

---

### 2.2. Блокировка транзитного трафика брандмауэром Keenetic (FORWARD DROP)
* **Диагностика:**  
  Таблица фильтрации Keenetic имеет политики по умолчанию:
  ```
  -P FORWARD DROP
  -P INPUT DROP
  ```
  Интерфейс `awgsus0` создан в пространстве Entware/Mihomo и не зарегистрирован в ядре NDMS. В результате ядро молча отбрасывало (DROP) все пересылаемые пакеты из `br0` в `awgsus0` и обратно.
* **Решение:**  
  Внесены явные правила принудительного пропуска:
  ```sh
  iptables -I FORWARD 1 -i awgsus0 -j ACCEPT
  iptables -I FORWARD 1 -o awgsus0 -j ACCEPT
  iptables -I INPUT 1 -i awgsus0 -j ACCEPT
  iptables -t nat -I POSTROUTING 1 -o awgsus0 -j MASQUERADE
  iptables -t mangle -I FORWARD 1 -o awgsus0 -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu
  ```
  Для защиты от стирания правил демоном NDMS при перезапуске сетевых интерфейсов создан исполняемый хук:  
  `/opt/etc/ndm/netfilter.d/63-awgm-susanin.sh`

---

### 2.3. Зависание стека Mihomo TUN (`stack: system` vs `stack: mixed`)
* **Диагностика:**  
  При конфигурации `stack: system` ядро Mihomo пыталось перехватывать системный TCP/IP стек, что приводило к взаимным блокировкам с сетевым стеком Keenetic и обрыву соединений.
* **Решение:**  
  В `internal/mihomo/config.go` параметр TUN переведён на `stack: mixed` (пользовательский стек gVisor для обработки TCP-сессий + системная маршрутизация UDP). Это обеспечило стабильную работу `curl -m 5 --interface awgsus0 https://ipinfo.io/ip` (выходной IP Финляндии `104.28.254.16`).

---

### 2.4. Отсутствие автовосстановления Susanin при старте демона
* **Диагностика:**  
  При перезапуске `awg-manager` (`/opt/etc/init.d/S99awg-manager restart`) или при обновлении IPK цепочки iptables, ipset и процесс `susanin-agent` не восстанавливались автоматически, так как вызов `Apply()` происходил исключительно по HTTP запросу из Web UI.
* **Решение:**  
  В `cmd/awg-manager/boot.go` добавлен метод `restoreAdaptiveRouting()`, вызываемый на путях:
  - `cold-boot` (холодный старт роутера)
  - `daemon-restart` (перезапуск демона или обновление IPK)
  - `post-restore` (восстановление из бэкапа)  
  Если в `susanin.json` стоит `enabled: true`, подсистема автоматически восстанавливает свои правила и запускает агента.

---

### 2.5. Конфликт параметров `ipset create` при повторном запуске
* **Диагностика:**  
  Скрипт `datapath.sh` создавал сеты с параметром `timeout 0`. При последующем вызове `datapath.go:EnsureSets` команда:
  ```sh
  ipset create susanin_ok_tcp hash:ip -exist
  ```
  завершалась с ошибкой `exit status 1: Set cannot be created: set with the same name already exists`, поскольку утилита `ipset` требует полного совпадения опций заголовка при наличии флага `-exist`. Это приводило к сбою `Apply()`.
* **Решение:**  
  В `internal/adaptiverouting/datapath.go` добавлена проверка наличия сета через `ipset list <name> -name`. Если сет уже существует в ядре, ошибка создания корректно игнорируется, и процесс инициализации продолжается.

---

### 2.6. Деградация координатора Mihomo (`recovery.marker`)
* **Диагностика:**  
  Прямое редактирование `config.yaml` на роутере вызвало несовпадение контрольной суммы (`active config digest mismatch`), из-за чего координатор Mihomo перешёл в режим защиты `degraded mode` и создал файл `/opt/etc/awg-manager/mihomo/recovery.marker`. В этом режиме все мутации возвращали `503 RECOVERY_REQUIRED`.
* **Решение:**  
  Выполнен штатный вызов административного API:
  ```json
  POST /api/mihomo/recovery/reconcile
  {"action": "regenerate_from_desired", "force": true}
  ```
  Координатор восстановил согласованное состояние, удалил `recovery.marker` и запустил Mihomo (PID 19176, `degraded: false`).

---

## 3. Внесённые изменения в кодовую базу

1. [internal/adaptiverouting/datapath_script.go](file:///e:/AWGM/awg-manager/internal/adaptiverouting/datapath_script.go)
   - Константа `EnhancedDatapathScript`: внедрена битовая маска `0x30000000`, сохранение меток политик Keenetic, добавлены правила брандмауэра и TCPMSS для `awgsus0`.
2. [internal/adaptiverouting/installer.go](file:///e:/AWGM/awg-manager/internal/adaptiverouting/installer.go)
   - Установка `EnhancedDatapathScript` в `/opt/susanin/tools/datapath.sh`.
3. [internal/adaptiverouting/datapath.go](file:///e:/AWGM/awg-manager/internal/adaptiverouting/datapath.go)
   - Имя цепочки выровнено на `ChainSusanin = "SUSANIN"` (для совместимости со статусом `susanin-agent`).
   - `EnsureSets`: добавлена устойчивость к уже существующим сетам с отличными параметрами.
   - `EnsureRules`: добавлены правила FORWARD/INPUT/MASQUERADE и генерация персистентного хука `/opt/etc/ndm/netfilter.d/63-awgm-susanin.sh`.
   - `Teardown`: добавлены шаги очистки правил FORWARD/NAT.
4. [internal/mihomo/config.go](file:///e:/AWGM/awg-manager/internal/mihomo/config.go)
   - Переключение `Tun.Stack` с `system` на `mixed`.
5. [cmd/awg-manager/boot.go](file:///e:/AWGM/awg-manager/cmd/awg-manager/boot.go)
   - Добавлен метод `restoreAdaptiveRouting()`, обеспечивающий автоматический запуск Susanin при старте сервиса.
6. [VERSION](file:///e:/AWGM/awg-manager/VERSION)
   - Версия увеличена до `2.17.68`.

---

## 4. Сборка и развёртывание пакетов

| Пакет | Размер (байт) | Назначение | Результат |
|---|---|---|---|
| `awg-manager_2.17.67_aarch64-3.10-kn.ipk` | 11 810 326 | Первичный релиз с битовыми масками и хуками netfilter | Установлен штатно |
| `awg-manager_2.17.68_aarch64-3.10-kn.ipk` | 11 808 748 | Релиз с автовосстановлением в `boot.go` и фиксом `EnsureSets` | Установлен штатно |

---

## 5. Текущее состояние роутера (`192.168.90.1`)

| Компонент | Статус | Примечание |
|---|---|---|
| **awg-manager** | ✅ Работает | Версия 2.17.68 (PID 18675), порт 2222 |
| **Mihomo Core** | ✅ Работает | PID 19176, `degraded: false`, порт 1099 активен |
| **Wireguard2 (AWGM)** | ✅ Работает | Порт 51820, пиры `FreeTurnDacha`, `Dacha` сохранены |
| **telemt** | ✅ Работает | Порт 8443 на `0.0.0.0` |
| **Цепочка Mangle PREROUTING** | ✅ Активна | Правило `-A PREROUTING -j SUSANIN` на месте |
| **Цепочка Mangle SUSANIN** | ✅ Активна | Битовые маски `0x30000000` загружены |
| **Таблицы ipset** | ✅ Загружены | 10 подсетей Telegram предзагружены в `susanin_ok_net` |
| **Таблица маршрутизации ip rule** | ✅ Активна | Правило 95 (`fwmark 0x20000000/0x30000000 lookup 105`) активно |
| **Скрипт защиты брандмауэра** | ✅ Активен | `/opt/etc/ndm/netfilter.d/63-awgm-susanin.sh` присутствует |

---

## 6. Завершающие шаги

1. Зафиксировать привязку маршрута таблицы 105 к интерфейсу выхода при старте Mihomo (`ip route replace default dev awgsus0 table 105`).
2. Провести пользовательскую проверку прохождения трафика на телефоне `HONOR-View20-Я` (`192.168.90.4`) при открытии Telegram.
