# Ревью Critical Fix Implementation Plan v3

Дата: 2026-09-11  
План: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
SHA-256: `9C02EC211830FB2F898C47F281B761244C743FAA58558B98D22F29F9CC35A42F`  
Размер: 50848 байт

## Вердикт

План v3 стал зрелым и **почти готов к реализации**, но требует ещё одной небольшой обязательной редакции. Основная архитектура теперь правильная: sanitized generation, per-generation vault, canonical pipeline, расширение существующей transaction machine, applied-generation state и injected acceptance tests.

Запускать кодирование можно после включения перечисленных ниже требований. Переписывать план с нуля больше не нужно.

## Что принято

- Secret extraction охватывает managed и raw layers.
- `raw.json` предполагается хранить sanitized, а vault — совместно с immutable generation.
- Redacted/private/runtime документы строятся разными безопасными ветками одного pipeline.
- Apply использует конкретную generation, а не урезанный legacy `Config`.
- Existing transaction state machine и `xraybin.TestConfig` переиспользуются.
- Head и applied generation разделены.
- Rollback больше не переключает head до транзакции.
- Acceptance действительно проверяет gen1 → gen2 → gen1.
- Frontend contract, fail-closed capabilities, lock и encoding учтены.

## Обязательные правки перед реализацией

### 1. Retention обязан сохранять applied generation

Сейчас `pruneGenerationsLocked(profileID, keepActiveID)` защищает только head. Типичный сценарий:

1. gen1 применена и записана в `applied-state.json`;
2. пользователь сохраняет более 10 новых редакций, но не применяет их;
3. retention удаляет gen1 вместе с её vault;
4. работающий Xray продолжает работать до рестарта, но private export, diff, restart recovery и rollback для applied generation уже невозможны.

Требуется:

- pruning получает protected generation set: head, applied generation, generations из незавершённых transaction manifests и явно закреплённые rollback targets;
- generation нельзя удалить, пока на неё ссылается applied-state или pending transaction;
- acceptance-тест создаёт `retention + 2` редакций после applied gen1 и доказывает сохранность/разрешимость gen1.

### 2. Удаление применённого профиля должно быть запрещено или транзакционно деактивировать runtime

`DeleteProfile` сейчас способен удалить generation/vault, на которую указывает `applied-state.json`. После этого running runtime становится «сиротой» и не может быть воспроизведён.

Требуется выбрать и зафиксировать безопасную семантику:

- предпочтительно вернуть `409 PROFILE_IS_APPLIED` и потребовать сначала применить другой профиль/остановить Xray;
- либо одной транзакцией остановить/переключить runtime, очистить applied-state и только затем удалить профиль;
- запрет должен проверяться на service level, а low-level store delete оставаться недоступным напрямую API;
- добавить тест удаления head-only, applied и referenced-by-pending-transaction профилей.

### 3. Миграция plaintext generations должна быть copy-on-write и fail-closed

План предлагает переписывать `managed.json`/`raw.json` in-place и при ошибке «log warning, skip, don't block startup». Это опасно:

- сбой между заменой plaintext на placeholders и записью vault/bindings повреждает generation;
- ошибка одной generation оставляет plaintext на диске, хотя система выглядит мигрированной;
- применённая legacy generation может оказаться частично изменённой.

Требуется:

1. никогда не мутировать существующую generation;
2. строить полностью новую migrated generation во временном каталоге;
3. fsync файлов и каталога, затем atomic rename;
4. переключать head только после полной проверки новой generation;
5. applied legacy generation либо мигрировать с согласованным обновлением applied-state, либо оставить immutable и пометить `migration_required`;
6. при ошибке профиля запрещать private/apply/update для него и показывать структурированный migration error; plaintext нельзя молча считать нормальным состоянием;
7. failure-injection tests после каждого файла и перед pointer switch.

### 4. Manifest должен хранить полные старые состояния, а не только generation IDs

`OldAppliedGenID` недостаточен:

- старый applied profile может отличаться от нового profile;
- applied-state мог отсутствовать;
- для восстановления нужны старые checksum и timestamp;
- `OldHeadGenID` должен иметь признак отсутствия head, а не только пустую строку.

В manifest нужны полные snapshots либо backup-файлы:

- `OldAppliedState *AppliedState` с nullable semantics;
- `OldHeadPointer *ActivePointer` с nullable semantics;
- backup текущего runtime и process-running state уже должны оставаться частью transaction;
- rollback восстанавливает точные bytes/metadata, а не конструирует приближённое состояние по ID.

### 5. Переход `applied_state_written` должен быть durable и однозначным

В псевдокоде ошибка `writeManifest` игнорируется (`_ = writeManifest`). Это запрещено на commit boundary. Также applied-state пишется до optional head pointer, а название состояния создаёт неоднозначность.

Требуется порядок:

1. runtime written/restarted/verified;
2. atomic applied-state write + directory fsync;
3. optional head-pointer write + directory fsync;
4. записать и fsync manifest state `metadata_committed`;
5. только затем final `committed`.

Ошибка любого manifest/pointer/state write должна вести к rollback либо `recovery_required`. Нельзя игнорировать ошибку журнала. Recovery может roll-forward только если checksum runtime, applied-state и expected head совпадают с manifest; иначе rollback/recovery-required.

### 6. Generation publication должна быть реально атомарной

Фраза «writing generation dir is one operation» неверна. `managed.json`, `raw.json`, `secrets.json` и vault создаются несколькими операциями. Нужен явный алгоритм:

1. создать sibling temp generation directory внутри того же filesystem;
2. записать все файлы с режимами 0700/0600;
3. проверить bindings, placeholders, checksums и отсутствие canary/plaintext;
4. fsync каждого файла и temp directory;
5. atomic rename temp dir → final generation dir;
6. fsync `generations/`;
7. только затем atomic pointer switch + fsync profile dir;
8. при конфликте generation ID — hard error, не overwrite.

Передавать отдельный `tmpVaultDir` недостаточно: vault и остальные generation files должны публиковаться одной directory rename.

## Уточнения тестов

- Disk-wide canary test должен исключать только содержимое `vault/*.secret`; имена, `secrets.json`, metadata, temp dirs и transaction backups не должны содержать plaintext.
- Проверить permissions после создания и после migration: directories 0700, files 0600 на Linux.
- Добавить тест отсутствующего/лишнего placeholder: каждый binding разрешается ровно один раз, каждый placeholder имеет binding, неиспользованных bindings нет.
- Key-policy не гарантирует нахождение произвольно названного секрета. Это нужно честно документировать: известные Xray schema paths покрываются строго, неизвестные поля — best effort. Для raw expert mode UI/API должно быть предупреждение, а private raw input нельзя отражать в логах/errors.
- Failpoint должен имитировать crash без выполнения обычного rollback в текущем процессе. Иначе проверяется error handling, а не recovery после crash.
- `FinalizePrepared` error нельзя игнорировать полностью: runtime уже committed, поэтому ответ должен отражать cleanup warning, а каталог должен быть подобран startup recovery.

## Разрешение на реализацию

После добавления шести обязательных пунктов выше план можно считать **одобренным условно** и запускать по этапам A–F без очередного полного архитектурного перепроектирования.

Порядок реализации оставить таким:

1. A — path/API hardening;
2. B — atomic generation + vault + safe migration;
3. C — canonical pipeline;
4. D — transactional runtime apply;
5. E — P1 correctness;
6. F — acceptance gate.

После каждого этапа — targeted report и зелёные тесты. IPK и deployment по-прежнему не выполнять.

## Сообщение агенту

> План v3 почти одобрен. Добавьте в него шесть обязательных уточнений из `XRAY_CRITICAL_FIX_PLAN_V3_REVIEW_2026-09-11.md`: retention защищает applied/pending generations; applied profile нельзя просто удалить; plaintext migration только copy-on-write и fail-closed; manifest хранит полные nullable old states; commit metadata state/fsync errors нельзя игнорировать; generation публикуется одной atomic directory rename после полной записи/fsync. Затем можно начинать этап A и последовательно идти до F. Для каждого этапа приложить команды и результаты тестов. IPK и deployment не выполнять.

