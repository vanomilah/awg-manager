# Аудит реализации Mihomo после Gemini Flash 3.8

Дата: 2026-09-21  
Ветка: `feature/mihomo-ai-proxyrt`  
Область проверки: фактический код и тесты в текущем рабочем дереве, а также отчёты из `reports/mihomo`.

## Итоговый вердикт

Реализацию **нельзя принимать как завершённую** и нельзя считать подтверждёнными заявления отчётов о «100% complete», «all tests green» и полной транзакционной безопасности.

В текущем состоянии обнаружены:

- 2 критических дефекта P0 в административном recovery;
- несколько дефектов P1 в проверке процесса/сокетов, degraded-gate, совместимости слотов и остановке процесса;
- недетерминированные тесты, результат которых зависит от наличия `mihomo` в `PATH` машины разработчика;
- несоответствие заявлению `git diff --check: 0 errors`;
- большое смешанное незакоммиченное рабочее дерево, в котором изменения Mihomo нельзя надёжно отделить от AI, CDN, Xray, WDTT и иных работ.

Сборка IPK, установка и любые изменения на роутерах в рамках этого аудита **не выполнялись**.

## Что проверено

### Команды

1. Таргетированные backend-тесты:

```text
go test -count=1 ./internal/mihomo ./internal/mihomonative ./internal/singbox/router ./internal/api ./cmd/awg-manager
```

Результат:

- `internal/mihomo` — **FAIL**;
- `internal/mihomonative` — PASS;
- `internal/singbox/router` — PASS;
- `internal/api` — PASS;
- `cmd/awg-manager` — PASS.

Падающие тесты:

- `TestGate4_VerifiableRollbackToLKG`;
- `TestGate4_RegenerateFromDesired`.

Ошибка:

```text
capture process identity: read /proc/12345/stat: no such file or directory
```

2. Тот же пакет `internal/mihomo` с `PATH`, из которого исключён пользовательский бинарник Mihomo:

```text
PATH=/usr/local/go/bin:/usr/bin:/bin go test -count=1 ./internal/mihomo
```

Результат: **PASS**.

Это доказывает зависимость тестов от окружения. В обычном WSL найден `/home/ivan/.local/bin/mihomo`; из-за этого конструктор выбирает реальный Linux verifier, хотя тест затем заменяет operator на fake PID 12345.

3. Frontend:

```text
npm run check
```

Результат: **PASS с 117 предупреждениями**, 0 ошибок. Среди предупреждений есть accessibility-проблемы в `MihomoNativeResourceCard.svelte`.

4. Проверка diff:

```text
git diff --check
```

Результат: **FAIL** — trailing whitespace и лишние пустые строки в нескольких файлах. Это противоречит `ACCEPTANCE_REPORT_2026-09-17.md`, где указано `git diff --check: 0 errors`.

5. Масштаб рабочего дерева:

- 65 изменённых tracked-файлов;
- примерно `+6335/-1145` строк;
- множество untracked-файлов с тестами и отчётами;
- dirty-состояние `wdtt` и `qwdtt`;
- tracked-файл `dev/null` размером 25 156 817 байт отмечен удалённым;
- в том же diff смешаны Mihomo, AI assistant, CDN, Xray, системные файлы и frontend.

Без фиксации базовой точки и разбиения изменений этот набор плохо ревьюится и небезопасен для слияния.

## Найденные дефекты

### P0-1. `regenerate_from_desired` может снять recovery без опубликованного поколения

Файл: `internal/mihomo/coordinator.go`, строки 2292–2344.

Проблемы:

1. Ошибка `PublishStagedBundle(...)` в строке 2325 полностью игнорируется.
2. Ошибки создания snapshot и вычисления store digest также игнорируются.
3. Ошибка записи `verified-active.json` игнорируется.
4. Метод вообще не вызывает `AdvanceLKGPointer(...)`, хотя комментарий обещает «Publish bundle and advance LKG».
5. После этого безусловно удаляются recovery marker, manifest и draft journal, состояние переводится в `idle`, а метод возвращает `nil`.

Последствие: при ошибке диска, fsync, rename, snapshot или publication runtime уже может быть перезапущен, но durable bundle/LKG/verified-active не подтверждены; несмотря на это система объявляет recovery успешным.

Требуемое исправление:

- не игнорировать ни одну ошибку после restart;
- публиковать bundle и отдельно атомарно продвигать LKG pointer;
- durable-запись verified-active должна быть обязательной;
- recovery marker удалять только после проверки всех durable postconditions;
- на любой ошибке после runtime side effect оставаться в `recovery_required`;
- добавить failpoint-тесты для snapshot, publication, pointer write/fsync и verified-active write.

### P0-2. `rollback_to_lkg` может объявить успех без надёжной фиксации результата

Файл: `internal/mihomo/coordinator.go`, строки 2161–2252.

Проблемы:

- legacy fallback игнорирует ошибки `StopAndWait` и `Start`, затем удаляет recovery marker;
- RuntimeOff-ветка игнорирует ошибки удаления active config и остановки процесса;
- marshal/write `verified-active.json` не являются обязательными: ошибки игнорируются;
- recovery artifacts удаляются независимо от успешности durable-записи verified-active;
- восстановление store выполняется до runtime, но при последующей ошибке нет завершённой транзакционной компенсации.

Последствие: система может перейти в `idle`, хотя процесс не восстановлен, active config не удалён либо verified-active не записан.

Требуемое исправление:

- оформить rollback как отдельную журналируемую state machine;
- проверять все ошибки stop/start/unlink/write/fsync;
- после восстановления повторно доказать process identity, listeners, config digest и store digest;
- удалять marker только последним durable-шагом.

### P1-1. UDP listener proof фактически не реализован

Файлы:

- `internal/mihomo/process_verifier_linux.go`, строки 71–86;
- `internal/sys/procnet/listener.go`, строки 54–107;
- `internal/singbox/router/mihomo_compiler.go`, строки 126–151.

`VerifySocketOwnership(... network string ...)` игнорирует аргумент `network` и всегда вызывает `FindListeningProcess`, который читает только `/proc/net/tcp` и `/proc/net/tcp6` и ищет TCP LISTEN state `0A`.

При этом compiler требует UDP listeners для TProxy, mixed и SOCKS. В результате:

- UDP-only listener не может быть корректно доказан;
- наличие TCP-сокета на том же порту может ложно подтвердить UDP-listener;
- заявление Gate 2 о точной проверке typed listener set не соответствует реализации.

Нужно добавить network-aware lookup для `tcp/tcp6/udp/udp6`, корректную обработку UDP state и тесты с разными владельцами TCP и UDP на одном порту.

### P1-2. CaptureIdentity fail-open при ошибке чтения cmdline

Файл: `internal/mihomo/process_verifier_linux.go`, строки 116–122.

Если `/proc/<pid>/cmdline` не читается, ошибка игнорируется и process receipt всё равно создаётся. Это противоречит заявленной строгой проверке PID + start ticks + executable + cmdline.

Нужно возвращать `ErrProcessProofFailed` при любой ошибке чтения cmdline и добавить fault-injection тест.

### P1-3. Выбор verifier зависит от ambient `PATH`, тесты недетерминированы

Файлы:

- `internal/mihomo/coordinator.go`, строки 81–95;
- `internal/mihomo/operator.go`, строки 548–565;
- `internal/mihomo/coordinator_legacy_test.go`, строки 159–174;
- `internal/mihomo/gate4_test.go`, строки 157–162.

`NewApplyCoordinator` вызывает `Operator.Binary()`. Тот выполняет `exec.LookPath("mihomo")`. Если binary случайно установлен в `PATH`, выбирается реальный verifier. Gate4 после создания coordinator заменяет operator на fake PID, но verifier остаётся реальным.

Нужно:

- передавать verifier явно в production wiring;
- в unit-тестах всегда явно передавать fake/noop verifier;
- не выбирать политику безопасности на основании ambient PATH;
- добавить тест, одинаково проходящий при наличии и отсутствии `mihomo` в PATH.

### P1-4. Global degraded mutation gate не охватывает refresh подписки

Файл: `internal/api/mihomo_handler.go`, строки 1048–1099.

Create/update/delete проходят через `withNativeMutation`, но `handleNativeSubscriptionRefresh` вызывает `RefreshNativeSubscription` напрямую. Затем метод выполняет runtime PUT и напрямую мутирует native store через `RecordSubscriptionRefresh`, не вызывая `CheckMutationAllowed` и coordinator.

Следовательно, заявление Gate 4 «all native mutation entry points fail closed» неверно.

Дополнительно refresh жёстко использует `http://127.0.0.1:9090` и не добавляет configured controller secret, что несовместимо с защищённым controller.

Нужно провести refresh через единый coordinator/gate и использовать controller client/operator с общей конфигурацией адреса и авторизации.

### P1-5. Совместимость sing-box DeviceProxy/Mihomo определяется поиском строки `1099`

Файл: `internal/singbox/router/service_lifecycle.go`, строки 595–619.

Проблемы:

- `bytes.Contains(data, []byte("1099"))` даёт ложные совпадения в любом поле/строке;
- ошибка чтения или разбора silently трактуется как отсутствие конфликта;
- при переходе на Mihomo DeviceProxy может быть выключен;
- при возврате на sing-box автоматически восстанавливается только `SlotRouter`, но не ранее автоматически припаркованный `SlotDeviceProxy`;
- нет persisted ownership/reason, позволяющего отличить автоматическую парковку от ручного выключения пользователем.

Нужно типизированно разобрать inbound listen ports, fail closed при невалидном активном slot и хранить причину/предыдущее состояние автоматической парковки.

### P1-6. `StopAndWait` нарушает собственную гарантию полного reap

Файл: `internal/mihomo/operator.go`, строки 584–655.

При `ctx.Done()` на строках 644–646 отправляется Kill и метод немедленно возвращает, не дожидаясь `done`. Это противоречит комментарию «fully reaped before returning» и может нарушить порядок restart/rollback.

Нужно после Kill иметь отдельный ограниченный reap timeout, дождаться `done` либо вернуть специальную ошибку, оставив coordinator в recovery_required. Контракт должен быть отражён в тестах.

### P1-7. Redaction пропускает обычные YAML/text secrets

Файл: `internal/mihomo/types.go`, строки 789–804.

Regex требует кавычку перед значением и закрывающую кавычку после него. Поэтому типичные формы вроде:

```text
secret: abc123
token=abc123
password: plain-text
```

не маскируются. Это не соответствует заявлению Gate 4 о безопасном evidence export.

Нужно тестировать и маскировать JSON, YAML, env/query-подобный текст, URL credentials, bearer tokens и многострочные ошибки. Лучше экспортировать структурированные allowlisted facts, а не редактировать произвольные строки regex-ами.

### P1-8. Bridge postcondition не доказывает owner и фактическое состояние

Файл: `internal/mihomo/coordinator.go`, строки 1593–1657.

После `PublishBridge` проверяется только `obs.Exists`. Не проверяются `OwnerUUID`, digest/параметры и готовность интерфейса. До публикации foreign-owner конфликт отвергается только если обе стороны имеют непустой UUID; существующий bridge с пустым owner может быть принят/перезаписан.

Это слабее заявленного ownership proof. Нужны строгие postconditions и явная политика миграции legacy bridges без owner.

### P2-1. Внутри текущего daemon epoch proof сводится к PID

Файл: `internal/mihomo/coordinator.go`, строки 181–217.

Комментарий говорит, что Generation authoritative, но код при совпадающем epoch проверяет только `running && pid == receipt.PID` и сразу возвращает `nil`. Generation, start ticks, executable и listeners не проверяются.

Нужно либо исправить контракт/комментарий, либо сравнивать generation и минимально start ticks, чтобы PID reuse/подмена процесса внутри жизни daemon не проходили.

### P2-2. Качество рабочего дерева и отчётов

- `git diff --check` не проходит;
- отчёт Acceptance утверждает обратное;
- frontend проходит с 117 warnings;
- отчёты утверждают 100% green, но стандартный запуск `internal/mihomo` падает;
- deployment reports фиксируют использование `--force-downgrade --force-overwrite`; это нужно отдельно согласовывать, а не называть безусловно «approved safe deployment»;
- `dev/null` удалён, subdirectories `wdtt`/`qwdtt` dirty, а unrelated изменения смешаны с Mihomo.

## Что выглядит реализованным полезно

Несмотря на блокеры, в коде есть значимый объём полезной работы:

- введены generation bundles, manifests, LKG pointer и strictfs операции;
- появились transaction/recovery states и bridge operation journal;
- compiler формирует typed required listeners;
- добавлены source vectors, pending-input coalescing и migration journal;
- прямые записи runtime config в старом service path в основном перенесены к coordinator;
- frontend компилируется без ошибок;
- целевые пакеты кроме `internal/mihomo` в проверенном наборе проходят.

Это хорошая основа, но сейчас безопасность state machine переоценена отчётами.

## Обязательный порядок исправлений

### Gate A — вернуть проверяемость

1. Разделить dirty tree на тематические коммиты или хотя бы сохранить точный patch/baseline.
2. Убрать случайное удаление `dev/null`; отдельно определить статус dirty `wdtt`/`qwdtt`.
3. Исправить `git diff --check`.
4. Сделать verifier injection детерминированным; добиться PASS с Mihomo в PATH и без него.

### Gate B — закрыть P0 recovery

1. Переписать `regenerate_from_desired` как fail-closed durable transaction.
2. Обязательно вызвать `AdvanceLKGPointer` только после опубликованного bundle и доказанного runtime.
3. Переписать `rollback_to_lkg` без ignored errors.
4. Добавить failpoint matrix на каждый filesystem/process шаг.
5. Marker удалять только последним шагом после повторной верификации.

### Gate C — runtime proof

1. Реализовать TCP/UDP-aware procfs ownership lookup.
2. Сделать cmdline proof fail-closed.
3. Исправить StopAndWait/reap contract.
4. Усилить bridge ownership/postconditions.

### Gate D — единый mutation boundary

1. Провести subscription refresh и все AI/remediation mutation paths через coordinator.
2. Добавить API matrix-тест: каждый mutating endpoint должен вернуть 503 `RECOVERY_REQUIRED` в degraded state и не иметь side effects.
3. Убрать hardcoded controller URL/auth из refresh.

### Gate E — совместимость движков

1. Заменить string search порта на typed parsing.
2. Реализовать reversible parking с ownership/reason.
3. Проверить циклы `sing-box -> mihomo -> sing-box` и `mihomo -> sing-box -> mihomo`, включая restart daemon и ручное изменение slot пользователем.

## Критерии повторной приёмки

Приёмку можно повторять только когда одновременно выполнено всё ниже:

1. `git diff --check` — PASS.
2. `go test -count=1 ./internal/mihomo` — PASS как при наличии `mihomo` в PATH, так и без него.
3. `go test -count=1 ./internal/mihomonative ./internal/singbox/router ./internal/api ./cmd/awg-manager` — PASS.
4. `go test -race ./internal/mihomo ./internal/mihomonative ./internal/singbox/router ./internal/api` — PASS.
5. `npm run check` — 0 errors; новые Mihomo-компоненты не добавляют accessibility warnings.
6. Failpoint-тесты доказывают, что ни одна ошибка публикации bundle, pointer, verified-active, stop/start и fsync не снимает recovery marker.
7. UDP listener ownership реально проверяется через `/proc/net/udp*`.
8. Все mutating API endpoints и AI actions fail closed в degraded state.
9. Recovery evidence не содержит секретов в JSON/YAML/plain-text вариантах.
10. Выполнен отдельный router smoke-test без `--force-reinstall`; любые иные force-флаги должны быть заранее явно согласованы.

## Решение

**Не сливать и не продолжать развёртывание текущего набора как завершённого.** Передавать другому агенту следует именно этот аудит и исправлять блокеры по Gate A → B → C → D → E, не добавляя новые функции до закрытия P0/P1.
