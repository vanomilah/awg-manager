# Ревью Critical Fix Implementation Plan

Дата: 2026-09-11  
План: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Проверка выполнена относительно `XRAY_PHASE_0_1_WALKTHROUGH_AUDIT_2026-09-11.md` и фактического кода в `E:\AWGM\awg-manager`.

## Вердикт

**В текущем виде план запускать в реализацию не следует. Нужна доработка.**

Направление выбрано правильно, но план не закрывает весь заявленный P0-контур и почти не охватывает P1, несмотря на название и Goal. Некоторые предлагаемые технические решения конфликтуют с уже существующей архитектурой и дадут тесты, которые либо не собираются, либо не доказывают безопасность результата.

## Блокирующие замечания к плану

### 1. SecretStore заявлен в Scope, но отсутствует в Proposed Changes

План говорит, что кандидат должен компилироваться «including secrets», и задаёт открытый вопрос Q3, но не предусматривает:

- изменение `ManagedConfig`, чтобы secret-bearing поля хранили `SecretRef`, а не plaintext string;
- extraction/staging секретов при create/import/update;
- разрешение ссылок при runtime compile/private export;
- совместный атомарный commit профиля и секретов;
- миграцию уже сохранённых plaintext generations;
- canary-тесты отсутствия секрета во всех обычных ответах и `managed.json`.

Без этого один из главных P0 остаётся неисправленным. Q3 не является открытым: секреты **нельзя** сохранять открытым текстом в `managed.json`; они разрешаются только при формировании runtime/private export.

### 2. Утечка raw overlay закрывается только для одного обработчика

План упоминает `GetProfile`, но исходный raw config сейчас также возвращают ответы create, update и import. Кроме того, внутри `ManagedConfig` могут остаться `RawDocument`, `StreamExtra`, `RawSettings` и `Extra`; текущий redactor не обрабатывает все эти контейнеры одинаково.

Нужно заменить требование на следующее:

- ни list/get/create/update/import/preview/redacted-export, ни ошибки/логи не содержат исходный raw overlay и canary-секрет;
- private export — единственный endpoint полного документа;
- DTO для входных данных и DTO обычных ответов должны быть разными типами;
- тест должен прогнать canary через каждый endpoint, а не только `TestGetProfile_NoRawOverlay`.

### 3. Предлагается дублировать существующий запуск Xray

План предлагает новый helper с `exec.Command` и предполагает бинарник в `PATH`. В проекте уже есть:

- `xrayserver/xraybin.InspectBinary`;
- `xrayserver/xraybin.TestConfig`;
- resolver и `Service.BinPath()`;
- существующая transaction state machine в `transaction.go`;
- процессный start/stop/runtime config path в `Service`.

Нельзя добавлять второй независимый механизм поиска бинарника, запуска теста и рестарта. Новый pipeline должен использовать внедряемый интерфейс поверх существующих resolver/tester/process operations и фактический `Service.BinPath()`. Иначе тест и production будут работать по-разному.

Ответ на Q1: **не PATH и не новый environment variable; использовать существующий resolver/BinPath**.  
Ответ на Q2: **не простой публичный stop-then-start; использовать одну транзакционную операцию, умеющую восстановить предыдущий runtime и состояние процесса**.

### 4. `ApplyCandidate` недостаточно определён

Фраза «atomically moves candidate to runtime, updates active generation pointer, and restarts» смешивает разные состояния:

- `active.json` сейчас указывает на последнюю сохранённую generation редактора;
- запущенный runtime может соответствовать другой generation;
- update/save уже переключает `active.json` до apply;
- при неудаче apply непонятно, должен ли откатываться edit pointer;
- отсутствует durable связь `profile_id + generation_id + runtime checksum`.

Нужно явно ввести модель:

- `head_generation` — последняя сохранённая редакция;
- `applied_generation` — реально применённая редакция;
- transaction manifest содержит profile ID, generation ID, checksum предыдущего и нового runtime, предыдущее состояние процесса;
- success публикуется только после проверки нового процесса;
- failure восстанавливает runtime, процесс и applied pointer;
- recovery после power loss принимает решение по durable manifest.

Новый `TxStateApplied` сам по себе проблему не решает. Ответ на Q4: использовать существующие детальные состояния; добавить состояние только если определена его recovery-семантика.

### 5. Raw overlay merge semantics всё ещё не определена

План обещает compile full JSON, но не говорит, как объединяются managed model и imported raw document. В коде уже есть `CompileWithBase`, однако production его не использует.

До реализации необходимо зафиксировать:

1. raw — это immutable base document или merge overlay;
2. какие секции принадлежат managed layer;
3. что происходит при конфликте полей;
4. как сохраняются неизвестные поля;
5. что preview/private export/apply вызывают один и тот же canonical compiler;
6. что итоговый документ валидируется после merge и secret resolution.

Без этого imported advanced Xray config снова потеряет настройки.

### 6. E2E-тест не должен требовать установленный Xray в обычном unit suite

План предлагает тест, который «will be run on the CI host where the binary is present». Такое предположение не подтверждено и делает suite нестабильным.

Нужно:

- unit/integration acceptance test с injected `ConfigTester`, `RuntimeWriter` и `ProcessController`;
- fake tester должен проверять, что получил именно полный compiled JSON;
- отдельный Linux/router integration test с настоящим бинарником допускается только под build tag/env opt-in;
- тест API должен проходить без системного Xray;
- отдельно проверить test failure, write failure, restart failure, health timeout, rollback failure и recovery after interruption.

Фраза «dummy secret via SecretRef» сейчас невыполнима: поля модели имеют тип `string`, а не `SecretRef`. Сначала требуется изменить модель/representation.

### 7. Исправление ID неполное

Строгая проверка ID нужна, но недостаточно вызвать `filepath.Clean` и `filepath.Rel` абстрактно. Проверка должна применяться к **каждому вычисленному destructive path** непосредственно перед rename/remove/write.

Обязательные случаи тестирования:

- `.`, `..`, пустая строка;
- `/`, `\\`, `../x`, `..\\x`;
- absolute Windows/POSIX paths;
- encoded URL variants на уровне HTTP router;
- symlink/reparse-point внутри store;
- prefix collision (`root` и `root-other`);
- подтверждение, что sentinel-файл за root не изменён после каждого вызова Delete/Rollback/Commit/Save.

Предпочтительно: строгий ID regex + `filepath.Rel` containment helper + запрет symlink components. Проверять substring `..` необязательно и может ошибочно отвергать безопасное имя `a..b`; важны компоненты и containment.

## P1, которые план обещает, но не включает

В план обязательно добавить отдельные этапы:

1. атомарный generation-based commit SecretStore вместо последовательного переноса файлов;
2. runtime rollback, а не только переключение profile pointer;
3. fail-closed capability discovery (`known:false`, а не XHTTP/REALITY=true при ошибке);
4. устранение nested `RLock` в `ListProfiles` и конкурентный regression test;
5. исправление повреждённой UTF-8 маски;
6. стабильные API error codes/statuses;
7. UI оставить отдельной следующей фазой, как и предлагает план.

Если задача действительно ограничивается только P0, нужно честно переименовать Goal и вынести P1 в обязательный follow-up с критериями. Сейчас формулировки противоречат содержанию.

## Исправленный порядок работ

### Этап A — немедленное security hardening

1. Central safe-ID + derived-path containment.
2. Regression tests destructive operations с sentinel outside root.
3. Удаление raw overlay из всех обычных responses.
4. Recursive canary leakage suite по API, storage и логам.

### Этап B — canonical document pipeline

1. Формально определить managed/base-overlay ownership.
2. Один метод `BuildRuntimeDocument(profileGeneration)`:
   - загрузка generation;
   - merge raw/base и managed;
   - secret resolution;
   - semantic validation;
   - canonical serialization/checksum.
3. Preview, private export и apply используют только этот метод.

### Этап C — настоящее хранение секретов

1. Ввести representation secret references в модели или отдельную secret binding map.
2. Extract/stage при import/create/update.
3. Atomic generation commit profile + secrets.
4. Migration plaintext data.
5. Private export разрешает секреты; обычные API никогда.

### Этап D — transactional runtime apply

1. Расширить существующую transaction machine для готового Document, не создавать параллельную систему.
2. Injected tester/writer/process controller.
3. Durable applied-generation metadata.
4. test -> backup -> atomic replace -> restart -> health verify -> commit.
5. Полный rollback/recovery состояния runtime и процесса.

### Этап E — P1 correctness

1. SecretStore atomicity.
2. Runtime rollback endpoint.
3. Fail-closed capabilities.
4. `ListProfiles` lock fix.
5. Encoding и API errors.

### Этап F — acceptance

Обязательный сценарий:

`import advanced JSON with unknown fields and canary secrets -> verify no leak in ordinary API/storage -> preview -> apply -> runtime semantic equality -> restart -> edit new generation -> rollback runtime -> inject failures at every transaction boundary -> recover previous runtime/profile/secrets`.

## Проверка и ограничения

- Использовать локальный Go cache/WSL для Linux-only packages; простой Windows `go test ./...` сейчас не является корректной единственной проверкой.
- Запускать targeted tests после каждого этапа, затем Linux/WSL full suite.
- Выполнить frontend tests/check только после изменения API contracts/clients.
- `git diff --check` должен быть чистым.
- Не собирать IPK, не выполнять deployment, не использовать `--force-reinstall` и cleanup.

## Текст, который можно передать реализующему агенту

> План пока не одобрен. Перепиши его по замечаниям `XRAY_CRITICAL_FIX_PLAN_REVIEW_2026-09-11.md`. Обязательно включи реальную интеграцию SecretStore, устранение raw leakage во всех endpoint-ах, canonical raw+managed+secrets compiler, расширение существующей transaction machine вместо второго механизма, durable applied-generation state и тесты без обязательного системного Xray. Добавь все перечисленные P1 либо честно ограничь текущую фазу P0 и создай обязательный P1 follow-up. Кодирование начинать только после повторной проверки обновлённого плана. IPK и deployment запрещены.

