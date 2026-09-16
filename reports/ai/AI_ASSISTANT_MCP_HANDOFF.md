# ИИ-помощник / MCP — состояние реализации

Дата: 2026-09-01

## Цель

Сделать чат внутри `Инструменты → Система → ИИ-помощник` агентным: модель сама выбирает безопасные инструменты AWG Manager, получает фактические результаты и только затем отвечает. Изменяющие действия должны требовать подтверждения пользователя.

## Реализовано в текущем незакоммиченном дереве

- Добавлен единый MCP-образный каталог `ToolDefinition` с JSON Schema, признаком `readOnly` и уровнем риска.
- Один и тот же `ToolRegistry` используется встроенным чат-агентом и MCP HTTP endpoint.
- Агентный цикл модели: до 4 последовательных вызовов инструментов, защита от повторения одного вызова.
- Для официальных Google Gemini и OpenAI Responses API добавлены нативные function/tool calls. Полный ответ провайдера возвращается в следующий ход: у OpenAI сохраняются reasoning/function-call items, у Gemini — content вместе с thought signatures.
- Результат инструмента передаётся обратно в нативном формате провайдера (`function_call_output` для OpenAI и `functionResponse` для Gemini).
- Провайдер-независимый текстовый envelope `TOOL_CALL: {...}`. Он работает с Gemini, OpenAI-compatible и локальными моделями без отдельного SDK.
- Инструменты первой очереди:
  - `domain.inspect`;
  - `routing.snapshot`;
  - `dns.inspect`;
  - `process.inspect`.
- Добавлены предметные read-only инструменты на внутренних Go-сервисах AWG Manager:
  - `engines.status` — выбранное ядро и runtime Sing-box/Mihomo;
  - `tunnels.list` — безопасная сводка VPN-туннелей;
  - `subscriptions.status` — подписки обоих движков без URL, headers и inline-конфигурации;
  - `connections.search` — поиск conntrack-соединений с атрибуцией маршрута.
- Следующая группа предметных инструментов:
  - `tunnels.status` — компоненты и runtime одного туннеля без его конфигурации;
  - `routing.explain_client` — устройство, NDMS-политика, client-route и live-соединения в одном снимке;
  - `logs.tail` — внутренние буферы `app`/`singbox` с allowlist bucket/group/level и усиленным маскированием URL, Bearer и типовых секретов.
- Runtime-инструменты прокси и правил:
  - `proxy.groups` — группы и выбранные узлы из Clash API выбранного ядра;
  - `routing.explain_rule` — штатный инспектор правил Sing-box или Mihomo для домена/IP, порта и TCP/UDP;
  - `outbound.test` — latency-test конкретного outbound через фиксированный URL и таймаут, без возможности передать модели произвольный адрес запроса.
- Сквозные DNS/runtime-инструменты:
  - `dns.explain` — результат штатного DNS-инспектора выбранного ядра для домена, клиента и типа запроса;
  - `connections.explain` — точное live-соединение из conntrack, его атрибуция и результат того же инспектора правил, который используется UI.
- Подтверждаемые исправления усилены:
  - action, target и риск проверяются серверным allowlist; title/description/risk от модели не используются как доверенные данные;
  - `subscription.update` подключён к штатному refresh-сервису Sing-box и runtime provider refresh Mihomo;
  - ложное `dns.flush`, которое фактически обновляло DNS-route subscriptions, больше не предлагается моделью;
  - UI показывает точные action/target/risk и требует отдельного второго подтверждения перед POST применения.
- Структурированный агентный контур:
  - `remediation.propose` доступен Gemini/OpenAI как native function call и малым моделям через текстовый tool fallback;
  - инструмент только создаёт серверно-валидированное предложение и не выполняет запись;
  - после подтверждения проверяется фактический runtime Sing-box/Mihomo, состояние туннеля или результат обновления подписки;
  - состояние ответа отличает чистую диагностику от сессии, в которой подтверждённое действие уже запускалось.
- Интерфейс собран в единую chat-first консоль: агент, включение ИИ и настройки находятся в общей шапке; история, tool steps и composer больше не выглядят разрозненными карточками; дублирующий ответ модели удалён.
- Предметные результаты формируются из тех же сервисов, что питают UI. Конфиги и ключи туннелей не передаются модели; URL в диагностических ошибках маскируются.
- MCP JSON-RPC endpoint `POST /api/ai/mcp`:
  - `initialize`;
  - `tools/list`;
  - `tools/call`.
- Endpoint защищён обычной авторизацией AWG Manager и пока публикует только read-only инструменты.
- В чате показываются выполняемые/выполненные шаги инструментов.
- Предложение исправления теперь показывается и после обычного чат-вопроса; применение остаётся отдельной кнопкой.

## Изменённые файлы этого этапа

- `internal/aiassistant/agent.go`
- `internal/aiassistant/agent_test.go`
- `internal/aiassistant/provider_tools.go`
- `internal/aiassistant/provider.go`
- `internal/aiassistant/provider_test.go`
- `internal/server/ai_tools.go`
- `internal/server/server_routes.go`
- `internal/api/mihomo_handler.go`
- `internal/api/subscription.go`
- `internal/aiassistant/tools.go`
- `internal/aiassistant/service.go`
- `internal/api/ai_assistant.go`
- `internal/api/ai_assistant_test.go`
- `frontend/src/lib/components/system/AIAssistantPanel.svelte`

В рабочем дереве есть другие пользовательские/предыдущие изменения. Не сбрасывать и не включать их механически в отдельный коммит AI.

## Проверки

- `go test ./internal/aiassistant` под WSL: PASS.
- `go test ./internal/api -run 'TestAIAssistant'` под WSL: PASS.
- `go test ./internal/api -run 'TestMihomo|TestSubscription'` под WSL: PASS.
- `go test ./internal/server` под WSL: PASS.
- После добавления `dns.explain` и `connections.explain`: `go test ./internal/aiassistant ./internal/server` и `go test ./internal/api -run 'TestAIAssistant|TestMihomo'` под WSL: PASS.
- После усиления remediation: `go test ./internal/aiassistant ./internal/server` и `go test ./internal/api -run 'TestAIAssistant|TestMihomo|TestSubscription'` под WSL: PASS.
- `npm run check`: 0 ошибок, 94 существовавших предупреждения.
- Собран проверочный пакет `dist/awg-manager_2.17.4+r24_aarch64-3.10-kn.ipk`; control-файл содержит версию `2.17.4+r24` и архитектуру `aarch64-3.10`.
- Полный `go test ./internal/api` сейчас падает на несвязанном инвентарном тесте: `AWGTunnel.ToggleLocked` не классифицирован в `TestTunnelUpdate_FieldInventoryComplete`. Это относится к другим изменениям рабочего дерева.

## Что обязательно сделать дальше

### Обновление 2026-09-02: model-first агент

- Для Google Gemini и OpenAI Responses локальный keyword-планировщик больше не запускает проверки до модели. Модель первой получает вопрос и сама выбирает native tools; локальные сценарии оставлены только fallback-режимом без native tool-capable модели.
- Добавлен `diagnostics.full`: модель может сама запустить штатную комплексную диагностику, если точечных инструментов недостаточно.
- Лимит агентного исследования увеличен с 4 до 8 вызовов инструментов на сообщение.
- Из Gemini-запросов удалены несовместимые `additionalProperties` и принудительный `thinkingLevel=minimal`.
- Итоговая инструкция запрещает выдавать сырой вывод команд вместо ответа и отдельно предупреждает, что `ip route get` не доказывает прохождение через TProxy.
- Post-action verification теперь различает `applied`, `verification_warning` и `verification_failed`; неуспешная проверка больше не показывается как успешное исправление.
- Добавлено подтверждаемое действие `routing.reapply`: оно повторно применяет конфигурацию выбранного движка (Mihomo или Sing-box), затем проверяет, что маршрутизация включена и выбранный runtime действительно активен. Прямого shell-доступа у модели нет.
- Добавлен транзакционный контракт `TransactionalActionExecutor` (`Snapshot`/`Rollback`) и первое обратимое действие `routing.switch_engine`. До смены ядра сохраняется прежнее значение, изменение выполняется через штатный `router.Service.UpdateSettings`, затем проверяются сохранённая настройка и runtime. При ошибке применения или failed verification прежнее ядро восстанавливается тем же сервисом.
- Добавлено `routing.switch_mode` для `off`, `tproxy`, `fakeip-tun`, `policy-tun`. Текущее состояние читается через `router.Service.GetSettings`, переключение и rollback выполняются через штатный `SwitchRoutingMode`.
- После добавления `routing.switch_mode` `gofmt` и `git diff --check` проходят, но повторный запуск WSL-тестов заблокирован локальной службой WSL (`E_ACCESSDENIED`). Последний успешный прогон до этого дополнения: `go test ./internal/aiassistant ./internal/server` — PASS. Следующему агенту обязательно повторить эти два пакета под Linux/WSL.
- Тесты: `go test ./internal/aiassistant ./internal/server` под WSL — PASS.
- IPK после этих изменений не собирался по просьбе пользователя.
- Следующий обязательный этап для настоящего rollback: добавить транзакционные snapshot/apply/verify/restore API для конкретных конфигурационных ресурсов. Не имитировать откат shell-командами.

1. Протестировать пакет на реальном роутере с текущим Google Gemini: вопросы должны заставлять модель самостоятельно вызывать `dns.inspect`, `routing.snapshot` и т.д.
2. Добавить нативные tool/function calls для OpenAI-compatible Chat Completions. Для официальных Gemini и OpenAI Responses это уже реализовано; текстовый `TOOL_CALL` остаётся fallback для маленьких/совместимых моделей.
3. Расширять allowlist подтверждаемых исправлений только через штатные Go-сервисы AWG Manager; не добавлять shell/direct-command в модельный контур.
4. Разделить предложения на `read`, `write-low`, `write-medium`, `direct-command`; write-инструменты никогда не выполнять из `tools/call` без одноразового подтверждения.
5. Добавить SSE/WebSocket события чата вместо polling 1,2 секунды: токены ответа, начало/конец tool call, запрос подтверждения.
6. Добавить OpenAPI-документацию endpoint и тесты `initialize`/неизвестного метода/неизвестного инструмента.
7. Для внешнего MCP-доступа (если понадобится) сделать отдельные от web-сессии отзываемые токены и права. Не открывать текущий cookie endpoint в WAN.

## Ограничения текущей версии

- Это MCP-compatible HTTP каталог/вызов инструментов. Официальные Google Gemini и OpenAI Responses используют нативные tool calls; остальные OpenAI-compatible и локальные модели пока используют текстовый fallback.
- История хранится в памяти процесса (до 12 сообщений), после перезапуска исчезает.
- Tool calls выполняются последовательно, максимум 4 на сообщение.
- Все опубликованные MCP tools только читают состояние. Исправления ограничены существующим `ACTION`/`RemediationProposal` и подтверждением в UI.

### Обновление 2026-09-02: OpenAI Chat Completions нативные инструменты и пакет 2.17.14

- Добавлены нативные tool calls (`tools` + `tool_calls`) для всех OpenAI Chat Completions провайдеров (`deepseek`, `openrouter`, `custom`, `ollama` с `/v1`).
- `buildChatCompletionsToolRequest` и `parseChatCompletionsToolTurn` обеспечивают нативный structured output и сохранение истории `role: assistant` (c `tool_calls`) + `role: tool` (с результатами).
- Скорректирован тайм-аут и обработка ошибок 503 (high demand) для Gemini.
- Пакет `2.17.14` собран и задеплоен на оба роутера (`192.168.90.1` и `192.168.50.1`).
- Тесты `internal/aiassistant` и `internal/server` под WSL: PASS.

Следующие шаги для действительно полного администрирования: транзакционное редактирование конфигов с отображением diff перед подтверждением; enable/disable автозапуска init.d; отдельная опасная сессия команд с подтверждением каждой команды. Не давать облачной модели бесконтрольный shell и не отправлять ей немаскированные файлы.

