# Ответ на ревью плана First-Run Wizards (Revision 2)

Дата: 2026-09-10  
Документ ревью: `E:\AWGM\awg-manager\FIRST_RUN_WIZARDS_PLAN_REVIEW_2026-09-10.md`  
Обновлённый план: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

---

## 1. Сводка внесенных доработок по замечаниям P0

### P0 #1: Серверное хранение плана и вычисление StateFingerprint
1. Клиент больше **не пересылает** тело плана обратно на сервер.
2. `POST /plan` создаёт серверную запись с `PlanID`, 10-минутным TTL, привязкой к сессии и `StateFingerprint`.
3. `StateFingerprint` вычисляется по:
   - Ревизиям конфигураций Xray, TG и Dispatcher;
   - Флагам `recoveryRequired` и активным runtime decisions;
   - Состоянию init-скриптов (`S99*` vs `S99*.disabled`);
   - Занятости целевых портов (проверяется через fail-closed procfs инспектор);
   - Идентификатору, статусу и ревизии выбранного egress-туннеля.
4. `POST /apply` передаёт только `{ "plan_id": "..." }`. Под `withIngressLock` сервер проверяет сессию, TTL и повторно считает `StateFingerprint`. При малейшем расхождении немедленно возвращается `409 PLAN_STALE` до начала мутаций. Повторный вызов одноразового плана возвращает `409 PLAN_ALREADY_USED`.

### P0 #2: Строгий CSRF-контракт для мутирующих эндпоинтов
1. `preflight` и `plan` — строго read-only.
2. `apply`, `cancel` и `reveal` требуют валидный dynamic session-bound CSRF token, полностью совместимый с существующим `requestWithCSRF` проекта.
3. Добавлены негативные тесты: отсутствие CSRF, невалидный токен, токен чужой сессии, валидный токен.

### P0 #3: Изоляция секретов от общего пула polling'а
1. Эндпоинт опроса статуса задачи `GET /jobs/{id}` возвращает **только** безопасный прогресс, шаг, фазу и флаг `ResultAvailable: bool`. Никакие UUID, VLESS URL, TG secrets в polling не возвращаются и не логируются.
2. Секреты выдаются **только** через выделенный эндпоинт `POST /jobs/{id}/reveal`:
   - Заголовки `Cache-Control: no-store, private`, `Pragma: no-cache`;
   - Проверка сессии владельца задачи;
   - Rate limit (не более 5 вызовов в минуту);
   - Автоматическое уничтожение временного секрета после истечения TTL (15 минут).

### P0 #4: Транзакционные границы и фазы Apply
1. Чёткие фазы: `preparing` $\to$ `applying` $\to$ `verifying` $\to$ `committing` $\to$ `succeeded`.
2. Ветви откатов: `cancelling` $\to$ `rolling_back` $\to$ `cancelled` и `failed` $\to$ `rolling_back` $\to$ `failed`.
3. «Точка невозврата»: отмена задачи до фазы `committing` запускает немедленный безопасный откат. При отмене во время атомарного `committing` отмена встаёт в очередь и завершает атомарную фиксацию (либо откат при ошибке), исключая split-brain состояние.
4. Долговечность: все транзакции опираются на существующий журнальный слой (`serveringress` / `xrayserver`), in-memory используется только для UI-polling. Взаимное исключение гарантируется через `withIngressLock`.

---

## 2. Учтенные доработки P1

1. **Единый fail-closed Listener Inspector (`internal/sys/procnet`):**
   - Функции `findListeningProcess`, `listenerLookup`, `ipToProcHex` выносятся в отдельный общий пакет `internal/sys/procnet`.
   - И `migration_saga`, и preflight мастера первого запуска используют один и тот же проверенный код без дублирования procfs-парсера.
2. **Богатый адаптер каталога выходов (`internal/serverwizard/egress`):**
   - `EgressOption` включает `ID`, `Name`, `Kind` (`interface`, `socks`, `router_outbound`, `direct`), `Available`, `DegradedMsg`, `SupportsTCP`, `SupportsUDP`, `Generation`.
3. **Вендор-независимые профили CDN (`internal/serverwizard/cdn`):**
   - `direct`, `get_only`, `get_post`, `websocket`, `xhttp_streaming` без торговых наименований CDN. Инструкции по Origin и DNS формируются расчётно из выбранного профиля.
4. **Явные границы Milestone:**
   - Пер-клиентская статистика Xray и полноценный режим Xray-клиента явно вынесены за рамки мастеров первого запуска в последующие этапы.

---

## 3. Поэтапный план выполнения

- **Этап 1:** Общие примитивы (`internal/sys/procnet`, DTO, `StateFingerprint`, адаптер egress, CDN-профили).
- **Этап 2:** Бэкенд мастера Xray и Telegram (`preflight`, `plan`, транзакционный `apply`, `jobs`, `reveal`).
- **Этап 3:** API эндпоинты, CSRF, интеграция в `server_routes.go`.
- **Этап 4:** Фронтенд-типы, API-клиент и базовые компоненты `WizardShell`, `PreflightList`, `PlanSummary`, `ApplyProgress`, `ConnectionResult`.
- **Этап 5:** Визард Telegram Proxy в UI (`TelegramProxyWizard.svelte`).
- **Этап 6:** Визард Xray VLESS в UI (`XrayWizard.svelte`).
- **Этап 7:** Интеграция в существующие карточки (`TelegramWebProxyCard.svelte` и `XrayServerCard.svelte`), тесты, сборка ARM64, svelte-check, git diff.
