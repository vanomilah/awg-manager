# Отчёт о проделанной работе: Susanin Adaptive Routing

**Дата:** 22 сентября 2026 г.  
**Версия продукта:** AWG Manager `v2.17.63`  
**Репозиторий:** `e:\AWGM\awg-manager`  
**Ветка:** `feature/mihomo-ai-proxyrt`  
**Целевой план:** [`SUSANIN_ADAPTIVE_ROUTING_IMPLEMENTATION_PLAN_2026-09-22.md`](file:///e:/AWGM/awg-manager/reports/susanin/SUSANIN_ADAPTIVE_ROUTING_IMPLEMENTATION_PLAN_2026-09-22.md)  
**Тестовый стенд:** Keenetic Hero 4G+ Дача (`192.168.50.1`, `aarch64`, Linux 4.9 / Entware)

---

## 1. Executive Summary

В AWG Manager успешно интегрирован **Susanin Adaptive Routing** в качестве **третьего полноправного и самостоятельного движка маршрутизации** (`RoutingOwner = susanin`), наряду с `Mihomo` и `sing-box`.

### Ключевая концепция и архитектурное разделение:
1. **Susanin не является надстройкой над Mihomo или sing-box.** Это независимый сетевой диспетчер.
2. **Разделение зон ответственности:**
   - **Susanin решает ЧТО маршрутизировать:** наблюдает за сбоями и успехами TCP/UDP соединений клиентов LAN, выявляет блокировки, временно проверяет их через альтернативный канал и запоминает работающие маршруты.
   - **Ядро-исполнитель решает КАК доставить трафик:** принимает направленный Susanin трафик через строго изолированный сетевой интерфейс (`awgsus0`) и пересылает его в выбранный прокси-узел, группу или системный туннель.
3. **Безопасность (Fail-Open):** При деградации или падении альтернативного канала связь клиентов не обрывается — трафик автоматически выходит напрямую в интернет (Direct WAN).
4. **Изоляция:** Задействован выделенный сетевой стек (таблица 105, приоритеты правил 95/96, маска меток `0x30000000`), не пересекающийся с NDMS, Zapret, SSTP и WireGuard.

---

## 2. Статус реализации по контрольным этапам (Gates 0 — H)

| Gate | Название этапа | Статус | Описание реализации |
|:---|:---|:---:|:---|
| **Gate 0** | Инвентаризация роутеров и исключение коллизий | **100% DONE** | Собраны полные дампы сетевого стека Home (`192.168.90.1`) и Dacha (`192.168.50.1`). Зарезервированы: routing table `105`, fwmark test `0x10000000`, fwmark ok `0x20000000`, mask `0x30000000`, rule priorities `95` (OK) и `96` (Test). Полная совместимость с NDMS политиками `ProxyMir`/`ProxyRu`. |
| **Gate A** | Управление прокси-группами | **100% DONE** | Реализована работа с proxy-группами Mihomo (`select`, `url-test`, `fallback`, `load-balance`) через `internal/mihomonative.Store`. Группы доступны как выходы независимо от активного routing owner. |
| **Gate B** | Доменная модель и каталог выходов | **100% DONE** | Создан пакет `internal/adaptiverouting/`. Реализованы хранилище настроек (`Settings`), операционный статус (`OperationalState`), типизированный каталог выходов (`EgressCatalog`) с поддержкой стабильных ID (`ResourceID`) и `ReferenceChecker` (защита от удаления используемых выходов). |
| **Gate C** | Изолированный TUN Executor | **100% DONE** | Реализован виртуальный сетевой интерфейс `awgsus0` (`198.18.0.1/30`) с параметрами `auto-route=false` и `auto-redirect=false`. Он не перехватывает общий трафик ОС. Реализованы 3 исполнителя: `MihomoExecutor`, `SingboxExecutor`, `SystemExecutor`. |
| **Gate D** | Менеджер процесса и наблюдатель | **100% DONE** | Реализован `ProcessManager`, компилирующий рабочую конфигурацию Susanin, управляющий фоновым процессом и ведущий учет адаптивных состояний (`ok`, `test`, `never`, `always`). Полный отказ от внешних bash-инсталляторов — сборка монолитна в IPK. |
| **Gate E** | Сетевой тракт (Owned Datapath) | **100% DONE** | Реализован `DatapathReconciler`: цепочка `AWGM-SUSANIN` в таблице `mangle`, ipset-наборы (`AWGM-SUS-OK-TCP`, `AWGM-SUS-OK-UDP`, `AWGM-SUS-TEST-*`, `AWGM-SUS-ALWAYS`, `AWGM-SUS-NEVER`). Полный Fail-Open при сбоях и чистый Rollback при выключении (Zero Residual Artifacts). |
| **Gate F** | Маршрутный координатор и API | **100% DONE** | Реализован взаимно-исключающий выбор `RoutingOwner` (`sing-box`, `mihomo`, `susanin`, `none`). Реализован REST API контроллер `adaptive_routing_handler.go` с 11 эндпоинтами управления. |
| **Gate G** | Веб-интерфейс (Svelte) | **100% DONE** | Разработана вкладка `SusaninAdaptiveTab.svelte` в разделе `Маршрутизация`: Hero-статус, Simple Mode (3 интуитивные карточки), Expert Mode (тайминги, TTL кэша, списки Always/Never), модальное окно просмотра изученных хостов. `npm run check` пройден без ошибок (0 errors). |
| **Gate H** | Стендовые испытания на роутере Дачи | **95% DONE** | Собрана версия `2.17.63`, развернута на `192.168.50.1` методом `opkg install --force-downgrade --force-overwrite`. Все API протестированы, каталог видит 8 выходов, rollback проверен. |

---

## 3. Архитектура и структура кода

### 3.1. Созданные модули Backend (`internal/adaptiverouting/`)
* **`types.go`**: Определение доменных структур `Settings`, `OperationalState`, `EgressRef`, `SourceScope`, `DetectionSettings`, `PersistenceConfig`, `LearnedEntry`.
* **`store.go`**: Потокобезопасное хранение пользовательских настроек в `susanin_settings.json` и операционного состояния с атомарной записью.
* **`catalog.go`**: Единый адаптер сбора доступных выходов из всех подсистем AWGM (системные туннели, прокси-ноды, группы Mihomo, outbounds sing-box).
* **`refchecker.go`**: Защитный барьер, предотвращающий случайное удаление сетевых интерфейсов или прокси-групп, на которые ссылается Susanin.
* **`executor_mihomo.go`**: Конфигуратор изолированного TUN `awgsus0` для Mihomo и маршрутизации входящих пакетов через правило `IN-NAME,awgsus0,<target>`.
* **`executor_singbox.go`**: Конфигуратор изолированного TUN `awgsus0` для sing-box.
* **`executor_system.go`**: Исполнитель для системных туннелей (прямая маршрутизация в kernel-интерфейс таблицы 105).
* **`datapath_reconciler.go`**: Низкоуровневый конфигуратор Linux netfilter (`iptables -t mangle`, `ipset`, `ip rule`, `ip route table 105`).
* **`process_manager.go`**: Управление жизненным циклом процесса Susanin и сбор метрик.
* **`installer.go`**: Проверка наличия и подготовка окружения на роутере.

### 3.2. Точки интеграции в ядро AWG Manager
* **`cmd/awg-manager/wiring_adaptiverouting.go`**: Сборка зависимостей, инициализация каталогов, регистрация обработчиков.
* **`internal/api/adaptive_routing_handler.go`**: REST API маршрутизатора Susanin.
* **`internal/singbox/router/service_mihomo.go`**: Поддержка sidecar-режима Mihomo при активном Susanin.

### 3.3. Веб-интерфейс Frontend
* **`frontend/src/lib/types/adaptiveRouting.ts`**: TypeScript интерфейсы доменной модели.
* **`frontend/src/lib/api/clientAdaptiveRouting.ts`**: Клиентские методы API (12 методов).
* **`frontend/src/lib/components/routing/SusaninAdaptiveTab.svelte`**: Полнофункциональный интерфейс вкладки Susanin с переключением Simple / Expert.
* **`frontend/src/routes/routing/+page.svelte`**: Монтирование вкладки в общий навигатор маршрутизации.

---

## 4. Результаты тестирования и стендовых проверок

### 4.1. Автоматизированные тесты
1. **Unit-тесты Go:**
   - `internal/adaptiverouting/...` — **100% PASS** (7 тестов: валидация настроек, сериализация, catalog resolution, refchecker, datapath formatting).
   - `internal/api/adaptive_routing_handler_test.go` — **100% PASS**.
2. **Статический анализ Frontend:**
   - `npm run check` — **0 errors**, 117 warnings в 24 файлах (все варнинги относятся к legacy CSS/a11y).
3. **Кросс-компиляция:**
   - `GOOS=linux GOARCH=arm64 go build` — успешная компиляция бинарного файла без ошибок и предупреждений.

### 4.2. Проверка на стендовом роутере Дачи (`192.168.50.1`)
1. **Развертывание пакета:**
   - Собран IPK: `awg-manager_2.17.63_aarch64-3.10-kn.ipk` (11,744,830 байт).
   - Установлен командой: `opkg install --force-downgrade --force-overwrite`.
   - Демон запущен: `version: 2.17.63`, статус `ok: true`.
2. **Проверка сохранения критических служб и сетевой связности:**
   - `Wireguard2` (порт 51820) — не повреждён, связь держится.
   - Смешанный порт Mihomo `1099` (Happ VLESS proxy) — активен и слушает.
   - Telegram Web Proxy `telemt` (`0.0.0.0:8443`) — работает.
   - AWGM Web UI (`192.168.50.1:2222`) — доступен.
3. **Проверка API Susanin:**
   - `/api/adaptive-routing/status` возвращает корректный статус.
   - `/api/adaptive-routing/egresses` возвращает 8 реальных выходов:
     - 3 системных туннеля (`Дом FT` на `opkgtun10`, `4vps wdtt` на `opkgtun20`, `Дом wdtt` на `opkgtun17`);
     - 2 индивидуальных Mihomo прокси (`🇳🇱 NED (Нидерланды)`, `🇷🇺 RUS (Россия)`);
     - 3 прокси-группы Mihomo (`Задний ход`, `Для Ютуба`, `Быстрый`).
4. **Проверка чистоты отката (Clean Rollback):**
   - При остановке Susanin (`enabled: false`) проверена таблица маршрутизации и iptables:
     - `ip rule show` не содержит правил 95 и 96.
     - `ip route show table 105` чиста.
     - Цепочка `AWGM-SUSANIN` отсутствует в mangle.
     - В сетевом стеке роутера не остается никаких паразитных записей.

---

## 5. Соблюдение строгих проектных ограничений

| Ограничение | Статус соблюдения |
|:---|:---:|
| **СТРОГИЙ ЗАПРЕТ: `--force-reinstall`** | **СОБЛЮДЕНО.** Использован исключительно `--force-downgrade --force-overwrite`. Ни один скрипт не содержит запрещённого флага. |
| **СТРОГИЙ ЗАПРЕТ: `--cleanup`** | **СОБЛЮДЕНО.** Никаких автоматических вызовов деструктивного режима очистки. |
| **Неприкосновенность `awg-manager-mihomo`** | **СОБЛЮДЕНО.** Работа велась исключительно в `e:\AWGM\awg-manager` на ветке `feature/mihomo-ai-proxyrt`. |
| **Неприкосновенность портов 1099, 51820, 8443** | **СОБЛЮДЕНО.** Все порты проверены через `netstat` на живом роутере. |
| **Защита домашнего роутера `192.168.90.1`** | **СОБЛЮДЕНО.** Все отладочные деплои и тесты выполнялись на тестовом роутере Дачи (`192.168.50.1`). |

---

## 6. Следующие шаги
1. Проведение 24-часового стресс-теста на стенде Дачи с включенной адаптивной маршрутизацией.
2. Проверка автоматического обучения на реальном LAN-трафике при сбоях прямых соединений (Direct → Test → Learned).
3. Фиксация коммита в ветке `feature/mihomo-ai-proxyrt`.
4. Подготовка релиза для переноса на домашний роутер (`192.168.90.1`).
