# Повторный аудит ремедиации Xray Safety Foundation

Дата: 2026-09-09  
Проверенный walkthrough: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`

## Вердикт

**Большая часть первоначальных исправлений действительно внесена, но ремедиация ещё не завершена и на роутер устанавливать её пока нельзя.**

Walkthrough ошибочно заявляет устранение всех дефектов и полный PASS. В текущем коде остаются два критических дефекта координатора, нарушение fail-closed запуска, несколько незавершённых safety-контрактов, а заявленный набор Go-тестов фактически падает.

## Что действительно исправлено

- Coordinator хранится в `app`, передаётся в `Server` и `XrayServerHandler`.
- `onReload` больше не управляет dispatcher и используется только как уведомление.
- Для coordinator и Xray используется общий transaction ID; ID также сохраняется в журнале.
- Добавлен `FinalizePrepared`, а `CommitPrepared` сохраняет snapshot.
- В журнал добавлены версия, checksum и структурная проверка.
- Добавлен структурированный `PIDRecord` и базовая fail-closed проверка `/proc`.
- Symlink path проверяется через `filepath.Rel`.
- Ошибки записи migration marker/runtime decision больше не игнорируются.
- Старый `XrayHandler` очищен от `pidof`, персонального IP и legacy runtime paths.

## Оставшиеся блокеры

### P0. Deadlock в `ResolveMigration`

`internal/serveringress/coordinator.go:569-584` захватывает `c.mu`. Затем:

- ветка `keep_legacy` вызывает `c.ApplyXrayConfig()` на строке 598;
- ветка `import_legacy_draft` вызывает `c.ApplyXrayConfig()` на строке 619.

`ApplyXrayConfig()` вызывает `Apply()`, а `Apply()` повторно пытается захватить тот же `c.mu` (`coordinator.go:290-292`). Go `sync.Mutex` не является reentrant, поэтому обе операции зависнут навсегда.

**Исправление:** не держать `c.mu` вокруг вызова публичного `Apply*`. Разделить locked/unlocked методы (`applyLocked`, `resolveMigrationLocked`) либо строить desired state и выполнять одну транзакцию под одним захватом. Добавить timeout-тест обеих веток.

### P0. Гонка и подмена конфигурации через `pendingXrayConfig`

`ApplyXrayConfig()` записывает `c.pendingXrayConfig = &cfg` до захвата mutex в `Apply()` (`coordinator.go:501-533`). При двух одновременных HTTP-запросах значения могут перезаписаться или обнулиться чужим `defer`. В результате один запрос способен применить конфигурацию другого; это также data race.

**Исправление:** не использовать разделяемое временное поле. Передавать candidate непосредственно в приватный transaction/apply method под mutex либо включить Xray candidate в аргументы операции. Добавить параллельный тест под `-race`, проверяющий две разные конфигурации.

### P0. Ошибка startup recovery не блокирует запуск сервисов

В `cmd/awg-manager/wiring_server.go:97-100` ошибка `StartupRecovery()` только журналируется. После неё код на строках 106 и далее всё равно читает конфигурацию и запускает Xray/TG/dispatcher напрямую.

При повреждённом журнале или recovery conflict это нарушает главное требование fail-closed: компоненты могут стартовать в частично применённой топологии.

**Исправление:** если recovery вернул ошибку, не запускать ни один managed ingress component и dispatcher. Состояние `RecoveryRequired` должно быть доступно UI/API. Boot-тест обязан проверять отсутствие вызовов `Start()` после recovery error.

### P1. Ошибки finalize снова игнорируются

- `StartupRecovery()` игнорирует результат `FinalizePrepared()` (`coordinator.go:208-215`) и затем архивирует журнал как committed.
- Обычный `Apply()` игнорирует finalize на строках 431-434.

Это позволяет заявить terminal success при невозможности завершить cleanup и проверить snapshot state.

**Исправление:** обрабатывать ошибку finalize. Для уже committed состояния не выполнять rollback, но сохранять recoverable terminal journal/состояние cleanup-required до успешной финализации.

### P1. Обычный rollback собирает не все ошибки

В `rollback()` учитываются ошибки Xray, dispatcher reconfigure и TG update, но результаты `dispatcher.Start()` и `dispatcher.Stop()` всё ещё игнорируются (`coordinator.go:473-476`). Ошибка записи error-state journal и ошибка `archiveJournal(..., "failed", ...)` также подавляются (`coordinator.go:446-449`, `498`).

**Исправление:** собирать все ошибки. Архивировать failed journal только после доказанного успешного восстановления; иначе сохранять активный журнал и `RecoveryRequired`.

### P1. Process identity не полностью соответствует заявленному контракту

`PIDRecord.BinaryFingerprint` записывается, но `verifyProcessIdentityLocked()` его не сверяет (`internal/xrayserver/service.go:410-433`). Если `childproc.StartTime()` не удалось прочитать при записи, сохраняется `StartTime=0`, после чего проверка start time пропускается (`service.go:440-448`, `414-420`). Ошибки marshal/write PID record также игнорируются.

**Исправление:** отсутствие start time или fingerprint считать ошибкой запуска/identity; сравнивать текущий fingerprint бинарника; возвращать ошибку из `writePIDRecord` и обрабатывать её до объявления процесса управляемым.

### P1. Rollback миграции может удалить существующие настройки

При ошибке marker/runtime decision миграция выполняет `os.Remove(settingsPath)` (`internal/xrayserver/migration.go:294-323`). Если settings-файл существовал до миграции, исходное содержимое не восстанавливается, а удаляется.

**Исправление:** до записи сохранить наличие, содержимое, права и checksum прежнего settings-файла; при ошибке восстановить его атомарно. Если файла раньше не было — только тогда удалить новый.

### P1. Strict coordinator dependency остаётся fail-open

`XrayServerHandler` при `coord == nil` возвращается к прямым `svc.UpdateConfig/Start/Stop/Restart`. Для заявленного единого control plane безопаснее считать отсутствие coordinator ошибкой wiring и отвечать `503`, иначе тестовая или будущая проводка легко вернёт обход журнала.

## Фактический результат тестов

Независимо запущено:

```bash
go test -race ./internal/cdndispatcher/... ./internal/tgwebproxy/... ./internal/xrayserver/... ./internal/serveringress/... ./internal/api/...
```

Результат:

- `internal/cdndispatcher`: PASS;
- `internal/tgwebproxy`: PASS;
- `internal/xrayserver`: PASS;
- `internal/xrayserver/xraybin`: PASS;
- `internal/serveringress`: PASS;
- **`internal/api`: FAIL**.

Падает `TestXrayStatusSnapshot`:

```text
expected path /test-path, got
expected CDNHost cl-test.edgecdn.ru, got
expected link to contain CDN host, got
```

Следовательно, утверждение walkthrough о полной верификации неверно. Вероятнее всего тест всё ещё создаёт package handler без подключённого managed service; контракт status endpoint и способ wiring необходимо согласовать явно.

Frontend check и ARM64 build в этом повторном аудите не запускались, поскольку уже имеется backend FAIL и P0-блокеры.

## Какие тесты необходимо добавить

1. `ResolveMigration(keep_legacy)` и `ResolveMigration(import_legacy_draft)` завершаются без deadlock.
2. Два параллельных `ApplyXrayConfig` под `-race`, каждый применяет именно свой candidate.
3. Boot при corrupt/conflicting journal не запускает Xray, TG и dispatcher.
4. Ошибка `FinalizePrepared` сохраняет состояние для повторного cleanup.
5. Ошибки dispatcher Start/Stop во время rollback не приводят к ложному архиву `.failed`/`.recovered`.
6. PID identity отклоняет отсутствующий start time и изменившийся binary fingerprint.
7. Ошибка migration marker/runtime decision восстанавливает существующий settings-файл байт-в-байт.
8. `TestXrayStatusSnapshot` снова проходит с новым resolver/service контрактом.

## Итог

Ремедиация заметно продвинулась и устранила исходные архитектурные заглушки, но статус «ВСЕ замечания устранены» ставить рано. Сначала необходимо закрыть три P0, оставшиеся P1 и добиться реального зелёного `go test -race`.
