# План реализации автономного ИИ-агента ремонта — Прогресс

**Дата начала:** 2026-09-16
**Источник:** `AI_ASSISTANT_AUTONOMOUS_REPAIR_IMPLEMENTATION_PLAN_2026-09-16.md`

---

## Обзор этапов

### Этап 0 — Рефакторинг существующих структур (без новых фич)
- [ ] Извлечь `ChangeType` enum и `ChangeProposal` struct в `change_proposal.go`
- [ ] Извлечь `AgentRun` struct и `RunState` в `agent_run.go`
- [ ] Извлечь `ActionAttempt` и `AgentStep` в `agent_run.go`
- [ ] Извлечь `SystemSnapshot` в `snapshot.go`
- [ ] Все существующие тесты продолжают проходить без изменений

### Этап 1 — AgentRun и возобновляемый loop
- [ ] Реализовать `AgentRun` с состояниями (investigating → plan_ready → awaiting_approval → applying → verifying → replanning → resolved/blocked/failed)
- [ ] `SystemEpoch` — счётчик мутаций, инкрементируется при каждом apply/rollback
- [ ] Сохранение `AgentRun` на диск (JSON) для восстановления после перезапуска
- [ ] Интеграция в `Service` — замена текущего плоского flow

### Этап 2 — ChangeProposal (типизированные изменения + diff)
- [ ] `ChangeType` enum: `file.patch`, `file.replace`, `service.restart`, `command.exec_safe`, `config.set`, `route.add/del`, `iptables.restore`, `dns.override`, `opkg.install`
- [ ] Генерация точного diff перед показом пользователю
- [ ] `Preconditions` — проверки перед применением (file digest, service state)
- [ ] `Revision` — защита от устаревших подтверждений
- [ ] Каждый тип имеет свой валидатор и executor

### Этап 3 — Snapshot/Rollback
- [ ] `SystemSnapshot` — снимки затронутых файлов, iptables-save, ip rule, service state
- [ ] Автоматическое создание snapshot перед apply
- [ ] Автоматический rollback при неудачной верификации
- [ ] Восстановление pending snapshot при перезапуске

### Этап 4 — Post-apply continuation loop (ГЛАВНОЕ НОВШЕСТВО)
- [ ] После неудачного apply/rollback — stderr, verification result, system state возвращаются модели
- [ ] Модель продолжает расследование автоматически (новый turn)
- [ ] `maxRetries` (3-5) для предотвращения бесконечных циклов
- [ ] Живой журнал каждого шага в WebSocket/чат

### Этап 5 — Action journal / live chat log
- [ ] `AgentStep` с временными метками и результатами
- [ ] Трансляция шагов в WebSocket в реальном времени
- [ ] Человекочитаемый формат в чате

### Этап 6 — iptables-restore error scenario
- [ ] `netfilter.snapshot` / `netfilter.explain_error` tools
- [ ] Парсинг `iptables-restore: line N failed` с контекстом строки
- [ ] Автоматическое построение исправленного restore input

### Этап 7 — Тесты и защита
- [ ] Тест: полный цикл propose → confirm → apply → verify → resolved
- [ ] Тест: apply fails → rollback → model re-investigates → new proposal
- [ ] Тест: stale confirmation (wrong revision) rejected
- [ ] Тест: recovery after restart picks up pending run
- [ ] Тест: maxRetries reached → blocked state

---

## Текущий статус

**Активный этап:** 0+1+2+3+4 (параллельная реализация core)
**Тесты до начала:** ✅ Все 34+ тестов проходят
