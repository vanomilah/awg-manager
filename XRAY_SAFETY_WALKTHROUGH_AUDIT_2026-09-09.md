# Аудит walkthrough: Xray Safety Foundation

Дата: 2026-09-09  
Проверенный отчёт: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`

## Вердикт

**Реализация не соответствует утверждённому плану и пока не готова к установке на роутер.**

Заявленный в walkthrough результат «успешно реализован и всесторонне верифицирован» преждевременен. Целевые тесты действительно проходят, но они проверяют преимущественно отдельные компоненты и mock-контракты. В фактической проводке приложения координатор не управляет API, а основной crash-safe rollback нарушен.

## Найденные проблемы

### P0. `serveringress.Coordinator` не включён в рабочий control plane

В `cmd/awg-manager/wiring_server.go:127-130` координатор создаётся как локальная переменная `ingressCoord`, используется один раз для `StartupRecovery()` и затем теряется.

Новые mutating endpoints создаются через `api.NewXrayServerHandler(s.xrayServerService)` в `internal/server/server_routes.go:499`. Сам `XrayServerHandler` напрямую вызывает:

- `h.svc.UpdateConfig()` — `internal/api/xray_server.go:30-44`;
- `h.svc.Start()/Stop()/Restart()` — `internal/api/xray_server.go:56-86`;
- прямой CRUD клиентов — `internal/api/xray_server.go:89-151`;
- прямое изменение migration/runtime decision — `internal/api/xray_server.go:200-272`.

Кроме того, callback `onReload` напрямую вызывает `cdnDispatcher.Reconfigure/Start/Stop` в `cmd/awg-manager/wiring_server.go:89-120`, также обходя координатор, его `flock` и журнал.

Следствие: единый control plane фактически не создан. Межкомпонентные изменения Xray/dispatcher/Telegram остаются незажурналированными и неатомарными.

**Требуется:** хранить координатор в долгоживущем `app`/`Server`, внедрить его в handlers и направить через него все topology-changing операции. Убрать самостоятельную мутацию диспетчера из асинхронного `onReload` либо превратить callback только в уведомление координатора.

### P0. Crash recovery не может восстановить Xray-транзакцию

Координатор создаёт ID вида `ing-*` и записывает его как `journal.TransactionID` (`internal/serveringress/coordinator.go:256-277`). Затем `xrayserver.PrepareCandidate()` независимо создаёт другой ID вида `tx-*` (`internal/xrayserver/transaction.go:67-68`). Связь между этими ID в журнале отсутствует.

При старте recovery вызывает:

`RollbackPrepared(j.TransactionID)` — `internal/serveringress/coordinator.go:209-215`.

Но snapshot расположен под ID, возвращённым `PrepareCandidate()`, поэтому после реального краша manifest не будет найден. Mock-тест искусственно предполагает, что оба ID совпадают, и скрывает дефект.

**Требуется:** либо передавать единый `txID` в `PrepareCandidate(txID, candidate)`, либо сохранять отдельный `ComponentTransactionIDs["xray"]` в журнале и использовать его при recovery. Добавить интеграционный тест с настоящим `xrayserver.Service`, а не только mock.

### P0. Xray удаляет snapshot до завершения общей транзакции

`xrayserver.CommitPrepared()` удаляет каталог транзакции сразу после локального коммита (`internal/xrayserver/transaction.go:252-255`). После этого координатор ещё применяет dispatcher и tgwebproxy (`internal/serveringress/coordinator.go:319-370`).

Если следующий компонент завершится ошибкой, `rollback()` вызывает `RollbackPrepared(xrayTxID)`, но snapshot уже удалён. Откат Xray невозможен.

То же делает crash recovery невозможным в окне между локальным коммитом Xray и terminal commit общей транзакции.

**Требуется:** разделить локальную транзакцию минимум на:

1. `PrepareCandidate`;
2. `CommitPrepared`, который применяет, но сохраняет snapshot;
3. `FinalizePrepared`, который удаляет snapshot только после устойчивого `phase=committed` общей транзакции;
4. `RollbackPrepared` для любого состояния до finalize.

### P1. Журнал не имеет заявленной checksum-защиты и recovery игнорирует ошибки

`TransactionJournal` не содержит checksum поля (`internal/serveringress/journal.go:19-30`), а `readJournal()` проверяет только корректность JSON (`journal.go:32-45`). Валидный, но частично/логически повреждённый JSON может быть принят как рабочий журнал.

В `StartupRecovery()` ошибки всех rollback/reconfigure/update операций игнорируются, после чего журнал безусловно архивируется как `.recovered` (`internal/serveringress/coordinator.go:209-244`). Это может объявить восстановление успешным, хотя состояние осталось частично изменённым.

Также игнорируется ошибка terminal archive после обычного commit (`coordinator.go:380-381`).

**Требуется:** добавить checksum/версию схемы и строгую валидацию обязательных полей и фаз; собирать ошибки recovery; архивировать как recovered только после доказанно успешного отката всех affected components; при любой ошибке сохранять активный журнал и выставлять `RecoveryRequired`.

### P1. Process identity реализована слабее заявленного fail-closed контракта

Walkthrough и план заявляют PID record с `pid`, `start_time`, `config_path`, `binary_fingerprint`. Фактически в PID-файл пишется только десятичный PID (`internal/xrayserver/service.go:838-840`, `transaction.go:194-197`, `transaction.go:319-326`).

`checkRunningLocked()` не сверяет start time и fingerprint. Если `/proc/<pid>/cmdline` прочитать не удалось, функция всё равно возвращает процесс как принадлежащий сервису (`internal/xrayserver/service.go:389-400`), то есть поведение fail-open. Затем этот PID может получить `SIGTERM`/`SIGKILL` (`service.go:646-663`). Повторной identity-проверки непосредственно перед каждым сигналом нет.

**Требуется:** реализовать структурированный PID record, строгую проверку PID + start time + ожидаемого config path/binary; ошибка чтения `/proc` должна запрещать сигнал; непосредственно перед `SIGTERM` и `SIGKILL` повторять identity check.

### P1. Legacy migration может вернуть успех после незаписанного terminal state

В `MigrateLegacy()` ошибки записи `migration-marker.json` и `runtime-decision.json` игнорируются (`internal/xrayserver/migration.go:293-303`). Функция возвращает успешный результат даже если init-script уже переименован, settings записаны, а маркер или решение не сохранены.

Проверка symlink target использует строковый `strings.HasPrefix(cleanTarget, cleanInitDir)` (`migration.go:120-127`), что не является корректной проверкой принадлежности каталогу: путь с общим строковым префиксом может находиться вне доверенного каталога.

**Требуется:** не игнорировать ни одну terminal write/rename ошибку, выполнять точный rollback; проверять принадлежность пути через `filepath.Rel` и отклонять `..`/absolute escape.

### P1. Старый Xray control plane устранён не полностью

Mutating routes `/api/xray/start|stop|restart|config` действительно больше не регистрируются. Однако оставшийся `XrayHandler` по-прежнему содержит старые небезопасные предположения:

- legacy paths `/opt/etc/xray-cdn` и `S99xray-cdn` — `internal/api/xray_handler.go:50-55`;
- `pidof xray` — `xray_handler.go:268-270`;
- hardcoded `UplinkHTTPMethod: "PUT"` — `xray_handler.go:65-70`;
- персональный hardcoded `ServerIP = "95.79.35.140"` — `xray_handler.go:120-127`.

Даже если оставлены только install/status endpoints, статус интеграции может показать чужой Xray-процесс и сформировать персональную/неверную ссылку. Это прямо противоречит заявлениям walkthrough о полном устранении `pidof xray` и hardcoded defaults.

**Требуется:** package/status handler должен использовать `xraybin.Resolver` и управляемый service status либо показывать только факт наличия бинарника. Он не должен строить серверную ссылку, определять произвольный `xray` через `pidof` или содержать персональные defaults.

## Проверка заявленной верификации

Независимо выполнена команда:

```bash
go test -race ./internal/xrayserver/... ./internal/cdndispatcher/... ./internal/serveringress/... ./internal/tgwebproxy/... ./internal/api/...
```

Результат: **PASS** для всех перечисленных пакетов.

Это подтверждает отсутствие обнаруженных race/test failures в текущем наборе, но не готовность архитектуры. Тесты не покрывают:

- реальную связку coordinator + `xrayserver.Service`;
- разные coordinator/Xray transaction IDs;
- сбой dispatcher после успешного локального Xray commit;
- crash после Xray commit до общего terminal commit;
- ошибку rollback одного из компонентов;
- реальную проводку HTTP handlers через coordinator;
- недоступный `/proc/<pid>/cmdline` и повторное использование PID.

В текущем аудите отдельно не повторялись `svelte-check` и ARM64 build: backend-блокеры уже запрещают признать этап завершённым.

## Обязательный порядок исправления

1. Сделать coordinator реальным владельцем topology mutations и внедрить его в API.
2. Ввести единый/связанный transaction ID и отложенный `FinalizePrepared`.
3. Исправить recovery: строгая проверка журнала, обработка всех ошибок, запрет ложного `.recovered`.
4. Реализовать полноценную process identity и fail-closed остановку.
5. Сделать legacy migration действительно транзакционной и symlink-safe.
6. Очистить оставшийся package/status Xray handler от `pidof`, legacy runtime assumptions и персональных defaults.
7. Добавить интеграционные и crash-point тесты перечисленных сценариев.
8. После исправлений повторить `go test -race`, frontend check и ARM64 cross-build.

## Итог

Walkthrough следует изменить со статуса «этап завершён» на **«реализован каркас, требуется исправление критических интеграционных и recovery-дефектов»**. До закрытия трёх P0 и трёх P1 установка на основной или тестовый роутер не рекомендуется.
