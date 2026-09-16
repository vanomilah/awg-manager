# Итоговое одобрение плана Phase 0/1: Xray Full Management

Дата: 2026-09-11  
Проект: `E:\AWGM\awg-manager`  
Ветка: `feature/mihomo-ai-proxyrt`  
Проверенный план: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
SHA-256 проверенной редакции: `C0BEEC00F95E580EF51E4A8DE63C5CDD3B4E92014B538703AB9A54577D4FB73A`

## Итоговый вердикт

План закрывает основные замечания предыдущих аудитов и **может быть передан в реализацию**.

Полная очередная переработка плана не требуется. Перед началом кодирования агент должен внести три точечных уточнения ниже либо принять их как обязательные implementation constraints и отразить в итоговом walkthrough.

Статус: **ОДОБРЕН К РЕАЛИЗАЦИИ С ТРЕМЯ ОБЯЗАТЕЛЬНЫМИ ОГРАНИЧЕНИЯМИ**.

## Что подтверждено планом

- `internal/xrayconfig` остаётся независимым и не создаёт циклический импорт.
- `serveringress.Coordinator` назначен единственным владельцем межсервисной транзакции.
- Определена единая durable state machine.
- Readiness выполняется до commit.
- Ошибка rollback приводит к `recovery_required`.
- Legacy-конфигурация мигрируется только в сторону полной managed model.
- Добавлены `SecretStore`, `ProfileStore`, migrations и version-aware capabilities.
- Публичный `SecretRef` больше не содержит checksum секрета.
- Storage размещается под canonical data root AWG Manager.
- Определены import, CRUD, preview, apply, history и recovery API.
- Перечислены реальные backend/API/frontend файлы, поэтому результат не должен снова ограничиться изолированной библиотекой.
- Предусмотрены race, failure-injection, crash-recovery и real-binary tests.

## Обязательные ограничения реализации

### 1. Нельзя хранить managed profile и raw overlay как два независимо заменяемых файла

План предлагает:

```text
profile_<id>.json
profile_<id>.raw.json
profiles.json
active_profile.json
```

Отдельные `rename` не создают атомарную транзакцию для всей группы. При отключении питания может сохраниться новая managed model и старый raw overlay либо наоборот.

Использовать generation/snapshot layout:

```text
profiles/<profile-id>/generations/<generation-id>/managed.json
profiles/<profile-id>/generations/<generation-id>/raw.json
profiles/<profile-id>/generations/<generation-id>/metadata.json
profiles/<profile-id>/active.json
```

Алгоритм:

1. Полностью записать новый generation-каталог.
2. Выполнить `fsync` каждого файла и каталога generation.
3. Атомарно заменить только маленький указатель `active.json`.
4. Выполнить `fsync` родительского каталога.
5. Старую generation сохранять до подтверждённого commit/retention cleanup.

Индекс профилей должен быть восстанавливаемым из generation metadata либо обновляться после active pointer. Он не должен быть единственным источником истины.

### 2. Приватный экспорт нельзя выполнять через `GET ?reveal=true`

Query string может попасть в browser history, access logs, reverse-proxy logs и telemetry. Ответ GET может быть закеширован. Для полного экспорта использовать отдельную изменяющую/чувствительную операцию:

```text
POST /servers/xray/profiles/{id}/export-private
```

Обязательные свойства:

- отдельное явное подтверждение пользователя;
- CSRF-защита и действующая admin session;
- `Cache-Control: no-store, private`;
- `Pragma: no-cache`;
- `Content-Disposition: attachment`;
- секреты не попадают в JSON application logs, events и audit payload;
- audit содержит только profile ID, actor, timestamp и факт экспорта;
- redacted export остаётся обычным безопасным endpoint;
- никакого секрета в URL или имени файла.

### 3. Keyed HMAC требует отдельного ключа либо должен быть исключён

План упоминает keyed HMAC metadata, но не определяет происхождение и жизненный цикл HMAC-ключа. Нельзя:

- выводить ключ из пароля/UUID;
- хранить ключ рядом с HMAC без отдельной модели угроз и называть это защитой от подмены;
- возвращать HMAC через API;
- ломать восстановление секретов при потере ключа.

Допустимые варианты:

1. На первом запуске создать случайный master integrity key в отдельном файле `0600`, включить его в backup/recovery contract и использовать только внутри `DiskSecretStore`.
2. Если цель — только обнаружить повреждение файла, отказаться от HMAC и полагаться на strict parsing, expected length/type и binary validation, не заявляя криптографическую аутентичность.

Для текущего локального router storage второй вариант проще и снижает риск потери всех секретов из-за рассинхронизации ключа.

## Уточнения, которые следует соблюдать при кодировании

- Canonical transaction enum должен быть единственным Go-типом, используемым manifest, API и recovery.
- Ошибка atomic replace до подтверждённого `active_replaced` должна приводить в `aborted`, если hash active config не изменился; при неоднозначности — в `recovery_required`.
- Все ожидания file lock и mutex должны поддерживать context cancellation/timeout и гарантированное освобождение.
- `prepared`, `tested`, `backup_ready` завершаются durable состоянием `aborted` до удаления staging.
- Все staged secrets принадлежат конкретному transaction ID.
- Profile ID, secret ID, generation ID и transaction ID валидируются и никогда не принимаются как filesystem path.
- Неизвестная schema version и неизвестное transaction state никогда автоматически не перезаписываются и не удаляются.
- Capabilities используются одновременно Validator, Compiler, API и UI; окончательную допустимость candidate подтверждает реальный `xray run -test`.
- `xray run -test` запускается с timeout и гарантированным завершением всего process group.
- Readiness проверяет все обязательные listeners из candidate, а не один исторический порт.
- Ошибки Xray и filesystem проходят структурированную санитизацию до записи в manifest/API/log/event.
- Full private runtime config существует только во временной приватной области и в активном runtime-файле с правами `0600`.
- Никакие тесты не должны использовать настоящие пользовательские ключи; применять только canary fixtures.

## Обязательные тесты сверх уже перечисленных в плане

1. Сбой между записью `managed.json` и `raw.json`: active generation остаётся прежней.
2. Сбой после полной записи generation, но до замены `active.json`: прежняя generation остаётся активной.
3. Сбой сразу после замены `active.json`: startup recovery выбирает согласованную generation.
4. Повреждённый/отсутствующий индекс профилей восстанавливается из metadata без потери active profile.
5. Private export возвращает `no-store`, не помещает секреты в URL, логи и audit payload.
6. Потеря/повреждение integrity metadata не удаляет секреты автоматически и приводит к диагностируемому безопасному состоянию.
7. Cancel/timeout во время ожидания каждого lock не оставляет file lock или активную транзакцию.
8. Неоднозначный результат rename/hash переводит систему в `recovery_required` и сохраняет backup.

## Критерий приёмки реализации

Реализация принимается только по фактическому коду и тестам. Наличие пунктов в walkthrough само по себе не считается доказательством.

Перед объявлением Phase 1 завершённой следующий аудит должен подтвердить:

1. Production API/runtime действительно вызывает Parser, Validator, Compiler, SecretStore, ProfileStore, diff и recovery.
2. Profile generation и active pointer crash-consistent.
3. Candidate проходит установленный Xray test mode.
4. Failure injection проходит для каждой точки state machine.
5. Canary secrets отсутствуют во всех публичных каналах.
6. Private export реализован отдельным защищённым POST endpoint.
7. Legacy migration выполняется однократно и не меняет runtime до apply.
8. Backend, race, frontend и OpenAPI checks проходят.

## Решение для агента

План можно запускать в работу сейчас. Три обязательных ограничения из этого документа считаются частью плана даже если исходный `implementation_plan.md` не будет переписан ещё раз.

Не собирать IPK и не выполнять деплой без отдельной команды владельца. Не использовать `--force-reinstall` и не запускать cleanup.

