# Проверка очередной редакции плана Phase 0/1: изменения не внесены

Дата: 2026-09-11  
Проект: `E:\AWGM\awg-manager`  
Проверенный файл: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Размер: `15478` байт  
SHA-256: `16A9BFB12F580CF6F55FB1E7C0F2649A7300D8DD0F00D2AFEC2F6FF6A3234830`

## Вердикт

План не был доработан по последнему ревью `XRAY_PHASE_0_1_REVISED_REMEDIATION_PLAN_REVIEW_2026-09-11.md`.

Текст по-прежнему соответствует предыдущей редакции и даже ссылается только на более ранний документ:

```text
XRAY_PHASE_0_1_REMEDIATION_PLAN_REVIEW_2026-09-11.md
```

Последующее ревью с оставшимися блокерами в план не включено. Отдавать этот файл в реализацию пока нельзя.

## Подтверждение отсутствия исправлений

В плане по-прежнему:

1. `SecretRef` только объявлен как структура, но не подключён к credentials и `SecretStore`.
2. Нет файлового secret storage с правами `0700/0600`, атомарной записью, resolver, ротацией и rollback.
3. `Shadowsocks Capability Matrix` остаётся фиксированным списком без `BinaryInspector` и определения версии Xray.
4. Startup recovery обрабатывает только `state == prepared`, а полноценная durable state machine отсутствует.
5. В тестовой последовательности всё ещё указано ошибочное `Commit -> Readiness -> Rollback`.
6. Владение транзакцией по-прежнему обозначено как `Coordinator.txMu / Service.mu`, без единственного orchestration owner и lock hierarchy.
7. Используется общий `config.json.bak`, а transaction-scoped immutable backup, `fsync` и защита от symlink не описаны.
8. Readiness по-прежнему ограничена проверкой listener и фиксированным таймаутом 5 секунд.
9. `FromManagedConfig` всё ещё обещает полную обратную конвертацию в узкий legacy `xrayserver.Config`.
10. Versioned schema migrations отсутствуют.
11. Overlay delete/zero/null semantics не определены.
12. `jsonEqual` всё ещё предлагает обычный `json.Unmarshal` в `interface{}` без `UseNumber`.
13. Нет структурированного разделения private runtime DTO и redacted API/history/diagnostic DTO.
14. Не описаны API endpoints и состояния UI для preview/apply/recovery/history.

## Что передать агенту

Нужно доработать **этот же** `implementation_plan.md` по документу:

```text
E:\AWGM\awg-manager\XRAY_PHASE_0_1_REVISED_REMEDIATION_PLAN_REVIEW_2026-09-11.md
```

Минимально обязательные изменения:

1. Добавить настоящий `SecretStore` и заменить открытые credentials ссылками либо явно описать безопасную transitional migration.
2. Добавить `BinaryInspector` и version-aware capability matrix.
3. Описать durable transaction states и recovery для каждой точки сбоя.
4. Исправить порядок на `Activate -> Readiness -> Commit`; при сбое — `Rollback -> RollbackReadiness`.
5. Назначить `serveringress.Coordinator` единственным владельцем межсервисной транзакции и определить lock hierarchy.
6. Сделать backup transaction-scoped, immutable и crash-safe.
7. Расширить readiness до PID/process/listeners/stabilization/API checks.
8. Ограничить legacy adapter одноразовой миграцией; не сжимать полную модель обратно в `xrayserver.Config`.
9. Добавить schema migrations и overlay field-presence/delete semantics.
10. Использовать `json.Decoder.UseNumber()` для semantic JSON diff.
11. Определить private/redacted DTO boundaries.
12. Добавить API и UI integration scope.

## Решение

Текущая редакция **не одобрена**. Повторное содержательное ревью имеет смысл только после фактического обновления плана по перечисленным пунктам.

