# First-Run Wizards: Final Implementation Acceptance

## Вердикт

**Реализация принята. Findings текущего цикла закрыты.**

Свежий `walkthrough.md` сопоставлен с фактическим кодом. Последний P0 race между post-boundary recovery и отменой устранён, обязательный regression test добавлен и проходит под race detector.

## Подтверждение последнего исправления

В `internal/serverwizard/service.go`:

- `ErrRecoveryRequired` обрабатывается первой веткой сразу после возврата coordinator;
- эта ветка не вызывает `PlanStore.Release` и не переводит job в `cancelled`;
- проверка `IsCancelRequested` выполняется только для ошибок, не являющихся recovery-required;
- при ошибке `MarkConsumed` внутри `OnPointOfNoReturn` job синхронно переводится в non-cancellable `recovery_required` до возврата ошибки.

В `internal/serverwizard/acceptance_test.go` присутствует `TestWizardService_ConsumeErrorAfterCommitBoundary_WithConcurrentCancelRequest`. Тест запрашивает отмену непосредственно внутри fault seam `MarkConsumed` и подтверждает:

- итоговый статус строго `recovery_required`;
- диагностическая причина сохранена;
- coordinator требует recovery;
- rollback после точки невозврата не выполняется;
- plan остаётся reserved и не становится доступным другой задаче.

## Повторно подтверждённые гарантии

- Component transaction IDs сохраняются с проверкой ошибок.
- Ошибка durable-записи `PhaseCommitting` выполняет pre-boundary rollback.
- Ошибки rollback persistence и archive сохраняются через `errors.Join` и выставляют `ErrRecoveryRequired`.
- Post-boundary finalize/write/archive failures не откатывают активированную конфигурацию.
- Consume проверяет reservation owner и идемпотентен для исходного job.
- Xray path нормализуется общей функцией dispatcher и совпадает в builder/coordinator/readiness.
- Readiness строго проверяет TCP sockets и `X-CDN-Route` для Xray/Telegram, включая оба маршрута shared hostname.

## Выполненная проверка

```bash
go test -count=1 -race ./internal/serverwizard/... ./internal/serveringress/... ./internal/xrayserver/... ./internal/tgwebproxy/... ./internal/cdndispatcher/...
```

Результат: **PASS** для всех пакетов:

- `internal/serverwizard`
- `internal/serverwizard/cdn`
- `internal/serverwizard/egress`
- `internal/serveringress`
- `internal/xrayserver`
- `internal/xrayserver/xraybin`
- `internal/tgwebproxy`
- `internal/cdndispatcher`

Новых блокирующих или существенных замечаний в проверенном scope не найдено.

## Границы принятия

Это принятие относится к транзакционной реализации First-Run Wizards и перечисленному целевому тестовому scope. IPK не собирался, установка на роутеры и реальная end-to-end проверка через CDN/клиентские приложения не выполнялись.
