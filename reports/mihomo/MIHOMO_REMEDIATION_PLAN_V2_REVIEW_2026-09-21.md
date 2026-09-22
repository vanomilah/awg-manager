# Финальное ревью Mihomo Remediation Plan — редакция 2

Проверен файл:

`C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

Дата проверки: 2026-09-21.

## Вердикт

Редакция 2 существенно лучше первой и корректно учитывает большую часть предыдущего ревью. Однако перед началом реализации необходимо внести ещё **6 точечных поправок**. После них план можно отдавать агенту в работу по Gate A → Gate E.

## Что исправлено корректно

- Подтверждено происхождение `dev/null`: файл действительно добавлен коммитом `48f3078c9` как бинарный артефакт размером 25 156 817 байт.
- Указана правильная сигнатура `AdvanceLKGPointer(genID, generation, rec, epoch)`.
- Добавлены durable recovery states и failpoint matrix.
- Учтён target generation для process receipt.
- Добавлен network-aware TCP/UDP procfs lookup.
- Добавлен отдельный reap timeout и `ErrProcessNotReaped`.
- Refresh переносится под общий mutation boundary.
- Добавлена идея матричного degraded-теста.
- DeviceProxy parking сделан обратимым и persistent.
- Evidence предлагается строить по allowlist, а regex оставить второй линией защиты.
- `wdtt` и `qwdtt` явно исключены из автоматических изменений.

## Оставшиеся обязательные поправки

### 1. Сигнатура final-commit helper не позволяет выполнить заявленный контракт

В плане предложено:

```go
finalizeVerifiedGenerationLocked(
    ctx context.Context,
    rec AppliedGenerationRecord,
    advanceLKG bool,
) error
```

Но helper должен:

- проверить durable intent;
- записать terminal state;
- удалить правильный journal/manifest.

Из предложенных аргументов невозможно понять, с каким transaction manifest или recovery journal работать. `ctx` при этом в перечисленных шагах не используется.

Нужно выбрать один из вариантов:

1. передавать typed commit journal/manifest в helper;
2. ввести интерфейс `CommitIntent` с методами verify/commit/cleanup;
3. оставить два тонких wrapper-а для apply и recovery над общим helper, который отвечает только за запись и верификацию record/pointer.

Предпочтительный вариант:

```go
func (c *ApplyCoordinator) persistAndVerifyGenerationLocked(
    rec AppliedGenerationRecord,
    advanceLKG bool,
) error
```

А переходы transaction/recovery journal и cleanup выполнять соответствующим wrapper-ом. Существующий `executeFinalCommitLocked` нужно рефакторить на этот helper, а не оставлять третью реализацию.

### 2. Для `regenerate_from_desired` не зафиксирован точный порядок side effects

В плане обязательно записать порядок:

```text
compile desired
-> snapshot store
-> validate candidate
-> durable RecoveryIntent
-> PublishStagedBundle
-> durable CandidatePublished
-> promote active config
-> durable ConfigPromoted
-> restart and verify target process/listeners
-> durable RuntimeVerified
-> persistAndVerifyGeneration
-> RecoveryCommitted
-> cleanup marker/journal
```

Bundle должен быть опубликован **до** изменения active config и рестарта. Иначе ошибка публикации снова возникает после runtime side effect.

Для `rollback_to_lkg` также нужен точный resume algorithm для каждого recovery state после перезапуска daemon. Простого перечисления состояний недостаточно.

### 3. План требует проверить у bridge поля, которых сейчас нет

Фактический `BridgeRef` содержит:

```text
ProxyIndex
ProxyInterface
KernelInterface
LegacyOwner
OwnerUUID
Generation
```

`ObservedBridge` дополнительно содержит только:

```text
Exists
Up
AssignedIP
```

В них нет listen port. Поэтому требование «проверять listen port и digest» сейчас невозможно выполнить напрямую.

План должен явно выбрать:

- либо расширить `BridgeRef`/`ObservedBridge` ожидаемыми runtime-параметрами и обновить schema/migration;
- либо проверять digest всех реально существующих полей и отдельно доказывать listener через `ListenerSpec`/procfs.

Рекомендуется второй вариант: bridge proof проверяет exact BridgeRef + `Exists` + `Up` + address, а порт доказывается process listener proof.

### 4. Матрица mutating endpoints всё ещё неполная

В списке отсутствуют как минимум:

- `PUT /api/mihomo/native/groups/{id}`;
- `DELETE /api/mihomo/native/groups/{id}`;
- `PUT /api/mihomo/native/rules/{id}`;
- `DELETE /api/mihomo/native/rules/{id}`;
- `PUT /api/mihomo/native/rules/order`;
- `POST /api/mihomo/native/rules/unsupported/delete`;
- router alias этого действия;
- apply/reload/settings/engine-switch endpoints, если они вызывают coordinator или runtime side effects.

Нельзя поддерживать матрицу вручную неполным перечнем. План должен потребовать:

1. перечислить все non-GET routes из registration code;
2. классифицировать каждую как read-only, coordinated mutation или отдельную административную recovery-команду;
3. тестировать всю категорию coordinated mutation.

AI actions также должны быть перечислены конкретно, а не одной строкой «AI-действия восстановления».

### 5. Parking record потерял поле ownership

В предыдущем ревью требовались `slot`, `previous_enabled`, `reason`, `owner`, `parked_at`, `config_digest`. В редакции 2 поле `owner` исчезло.

Нужно добавить, например:

```text
owner = "mihomo_compatibility_manager"
schema_version
```

Также обязательны тесты:

- slot был вручную выключен до перехода — не включать его обратно;
- malformed/missing active config — fail closed;
- пользователь изменил slot/config после парковки — не перетирать изменение;
- повторный reconcile — idempotent.

### 6. Команда проверки без Mihomo удаляет Go из PATH

В плане указано:

```bash
PATH=/usr/bin:/bin go test -count=1 ./internal/mihomo
```

В текущем WSL Go расположен в `/usr/local/go/bin`, поэтому эта команда может завершиться `go: command not found`, а не проверить отсутствие Mihomo.

Исправить на:

```bash
PATH=/usr/local/go/bin:/usr/bin:/bin go test -count=1 ./internal/mihomo
```

Для варианта с Mihomo:

```bash
PATH=/home/ivan/.local/bin:/usr/local/go/bin:/usr/bin:/bin go test -count=1 ./internal/mihomo
```

Следует также добавить `-count=1` в race-команду, чтобы исключить влияние test cache:

```bash
go test -count=1 -race ./internal/mihomo ./internal/mihomonative ./internal/singbox/router ./internal/api
```

## Условие допуска к реализации

После внесения этих шести поправок план можно запускать в работу.

Реализацию выполнять строго по одному Gate за раз. После каждого Gate агент должен:

1. перечислить изменённые файлы;
2. показать выполненные тесты и их фактический вывод;
3. не переходить к следующему Gate при красном тесте;
4. не собирать IPK и не выполнять deployment до отдельного разрешения пользователя;
5. не применять `--force-reinstall`, автоматический `--cleanup` и не изменять `wdtt`/`qwdtt`.

## Итог

Статус редакции 2: **условно одобрена после шести обязательных правок**.
