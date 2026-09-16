# Отчет по исправлениям после слияния веток (AWG Manager v2.17.12)

**Дата:** 1 сентября 2026 г.  
**Версия релиза:** 2.17.12  
**Целевые платформы:** aarch64-3.10, mips-3.4, mipsel-3.4  

---

## 1. Фронтенд (Svelte 5 / TypeScript)

### 1.1. Устранение ошибок компиляции и типизации (`svelte-check`)
В процессе слияния кодовой базы были устранены **110 ошибок сборки**. Текущий `svelte-check` завершается без ошибок; оставшиеся CSS/a11y-предупреждения не блокируют сборку:
- **`frontend/src/routes/+page.svelte`**:
  - Подключен и подписан стор `mihomoNativeResources` для реактивной синхронизации нативных ресурсов Mihomo.
  - Объединены списки ресурсов: карточки отдельных прокси (TrustTunnel FIN/RUS/USA) и подписки формата Mihomo Provider (ABV) теперь выводятся совместно с ресурсами Sing-box.
  - Главная вкладка переименована в **«Прокси»**, бейджи динамически суммируют количество активных туннелей и подписок обоих движков (`badge: singboxCount + mihomoCount`).
  - Пропсы `mihomoProxies`, `mihomoSubscriptions`, `mihomoRuntimeProxies`, `mihomoRuntimeProviders` корректно проброшены в секции `SingboxTunnelsTabSection` и `SubscriptionsTabSection`.
- **`frontend/src/lib/components/sb-router/FlowGraph.svelte` & `FlowGraph.test.ts`**:
  - Добавлена защита опционального чейнинга `reloadStatus?.()` и `reloadSettings?.()`, предотвращающая runtime-падения при незагруженном контексте родительского роутера.
- **`frontend/src/lib/components/proxy/KillPortSection.svelte` & `KillPortSection.test.ts`**:
  - Приведен к единому контракту интерфейс кнопки `ListenPortKillButton` с диалогом подтверждения завершения процессов.
- **`frontend/src/lib/components/sb-router/expertPanelCollapseStore.ts`**:
  - Добавлено свойство `proxyGroups: false` в структуру сохраненного состояния свернутых блоков экспертной панели.
- **`frontend/src/lib/components/system/files/FilePropsModal.svelte`**:
  - Устранена гонка при редактировании файлов: переменная `entry` сохраняется локально до вызова `onClose()`, что предотвращает передачу `null` в обработчик `onEdit`.
- **`frontend/src/lib/components/sb-router/ProxyGroupEditModal.svelte` & `ProxyGroupsCompact.svelte`**:
  - Исправлены предупреждения реактивности Svelte 5 (корректная инициализация `$state` и вычисляемых `$derived`).

---

## 2. Модульное и интеграционное тестирование (Vitest)

Проведен полный прогон тестового набора компонентов и утилит интерфейса:
- **192 тестовых файла** — 100% успешно (PASS).
- **1808 тестов** — 100% успешно (PASS).
- Устранены падения в тестах:
  - `KillPortSection.test.ts`
  - `FlowGraph.test.ts`
  - `addWizardStore.test.ts`
  - `expertPanelCollapseStore.test.ts`
  - `clientMihomo.surface.test.ts`

---

## 3. Бэкенд (Go) и оркестрация движков

### 3.1. Устранение коллизии запуска Sing-box vs Mihomo
- **Файл:** `cmd/awg-manager/singbox_core.go` (строки 217–221).
- **Проблема:** При включенной маршрутизации и выбранном движке `routingEngine: "mihomo"` сторожевой таймер Sing-box (`singbox-watchdog`) активировал слот маршрутизатора в `sbOrch`, запускал фоновый процесс `sing-box` и блокировал сетевые порты перехвата трафика (TProxy/Redirect) и порт REST API.
- **Решение:** Добавлена проверка `isSingboxRouter := curSettings.SingboxRouter.Enabled && curSettings.SingboxRouter.RoutingEngine != "mihomo"`. При работе движка Mihomo слоты маршрутизации Sing-box отключаются.

### 3.2. Стабильность инициализации Mihomo
- **Файл:** `internal/mihomo/operator.go` (строка 182).
- **Проблема:** Таймаут ожидания готовности REST-контроллера Mihomo (порт 9090) составлял 8 секунд, чего было недостаточно при начальной загрузке больших баз `GeoIP.dat` (17 МБ), `GeoSite.dat` (4 МБ) и сотен правил на слабых процессорах роутеров.
- **Решение:** Таймаут готовности увеличен до 30 секунд.
- **Управление бинарным файлом:** AWG Manager запускает и останавливает Mihomo напрямую; отдельный init-скрипт не требуется. Текущие IPK не включают бинарник Mihomo; на стенде используется внешне установленный `v1.19.29 linux arm64`.

---

## 4. Замеры задержек и группы прокси

- **Проблема:** В секции «ГРУППЫ ПРОКСИ» («Задний ход», «Самый быстрый», «Выбор», «Баланс») все узлы отображали статус `timeout`, а запросы к `/api/mihomo/clash/proxies/{name}/delay` завершались ошибкой `502 Bad Gateway`.
- **Решение:** После запуска REST-контроллера Mihomo и устранения конфликтов портов замеры задержек функционируют штатно.
- **Результаты замеров на стенде:**
  - `awg-sys-Wireguard1` (`next`): **192 ms**
  - `awg-sys-Wireguard3` (`SW`): **214 ms**
  - `🇫🇮 FIN (Финляндия) (Premium)`: **190 ms**
  - `sub-06d59bc1` (`VOX`): **238 ms**
  - `🇷🇺 RUS (Россия) (Premium)`: **245 ms**
  - `🇺🇸 USA (США) (Premium)`: **626 ms**
  - `Mihomo: ABV`: **128 ms**
  - `DIRECT`: **346 ms**

---

## 5. Сборка и развертывание

1. **Собраны пакеты IPK версии 2.17.12:**
   - `dist/awg-manager_2.17.12_aarch64-3.10-kn.ipk`
   - `dist/awg-manager_2.17.12_mips-3.4-kn.ipk`
   - `dist/awg-manager_2.17.12_mipsel-3.4-kn.ipk`
2. **Установка на роутеры:**
   - Серверный роутер: `192.168.90.1` (Keenetic Titan, aarch64, KeeneticOS 5.1).
   - Клиентский роутер: `192.168.50.1` (Keenetic, aarch64, KeeneticOS 5.1).
   - Развертывание выполнено стандартной командой `opkg install` **без использования `--force-reinstall`**.
   - Процессы `awg-manager` и `mihomo` запущены, веб-интерфейс доступен по адресу `http://192.168.90.1:2222` и `http://192.168.50.1:2222`.

---

## 6. Повторная нормализация слияния

- Устранено расхождение index/worktree: более 1300 случайно staged-файлов `vendor` заменены фактическим набором новой зависимости `github.com/BurntSushi/toml`.
- Все отслеживаемые изменения и новые интеграционные тесты приведены к единому staged-состоянию; смешанных `MM`/`AM` файлов нет.
- `git diff --check` и `git diff --cached --check` прходят.
- `npm run check` и production-сборка frontend прходят.
- В production-bundle прверено наличие `mihomoStatus` и `/mihomo/status`, что закрывает runtime-регрессию после слияния.
- Целевые Go-пакеты `cmd/awg-manager`, `internal/mihomo`, `internal/api` и `internal/singbox/router` прходят в Linux/WSL.
- Полный `go test ./...` пршел пакеты до `internal/downloader`, но не завершился в отведенные 300 секунд; его нельзя считать полностью подтвержденным.
