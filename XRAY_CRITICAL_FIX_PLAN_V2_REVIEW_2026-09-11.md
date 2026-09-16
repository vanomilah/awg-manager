# Ревью Critical Fix Implementation Plan v2

Дата: 2026-09-11  
Проверенный файл: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
SHA-256: `913F237D69F2352C03064604B4F3CC60E7FE748F63C42E5502534C8AAFFA199B`  
Размер: 29370 байт

## Вердикт

Версия v2 **существенно улучшена**, предыдущая рецензия в основном учтена. Однако начинать реализацию всего плана пока рано: остаются несколько блокирующих ошибок в модели raw-секретов и транзакционных границах. После перечисленных ниже правок план можно будет одобрить.

## Что исправлено правильно

- Применяется строгий Safe-ID и предусмотрена derived-path containment.
- Утечка `raw_overlay` рассмотрена для create/get/update/import.
- Предлагается использовать существующие `Service.BinPath()`, `xraybin.TestConfig` и transaction state machine.
- Введены injected interfaces для тестирования без системного Xray.
- Зафиксирована семантика raw document как base и managed-wins merge.
- Разделены head generation и applied generation.
- Добавлены fail-closed capabilities, исправление nested `RLock`, UTF-8 mask и API error codes.
- UI, IPK и deployment корректно исключены из текущей фазы.

## Блокирующие замечания

### P0. `raw.json` продолжит хранить plaintext-секреты

Этап C извлекает секреты только из `ManagedConfig`. Но при import исходный полный документ сохраняется в `raw.json`; в нём останутся те же UUID/password/private keys и любые неизвестные secret-bearing поля. Acceptance проверяет только `managed.json`, поэтому эта утечка останется незамеченной.

Исправление:

1. После parse импортированный base document также должен проходить рекурсивную secret extraction.
2. В `raw.json` сохраняется только sanitized base с placeholders/references.
3. Binding должен указывать слой (`managed`/`raw`) и устойчивый JSON path.
4. Canary-тест обязан рекурсивно прочитать **все файлы generation и SecretStore metadata**, кроме защищённого secret payload, и доказать отсутствие plaintext.
5. Неизвестные поля raw тоже должны рассматриваться как потенциально секретные по key policy (`password`, `token`, `privateKey`, `authorization`, UUID/ID в протокольных контекстах).

Без этого утверждение «секреты отдельно» остаётся ложным.

### P0. `BuildRedactedDocument` в предложенном виде может раскрыть raw-секреты

Фраза «не resolve secrets, redact ManagedConfig before compilation» недостаточна: `CompileWithBase` подмешивает raw base, где могут находиться секретные значения. Кроме того, placeholders сами раскрывают внутренние secret IDs.

Исправление:

- строить единый canonical document из sanitized layers;
- для private runtime разрешать bindings в памяти;
- для redacted export/preview либо подставлять `[REDACTED]` во **все bindings обоих слоёв**, либо рекурсивно редактировать уже собранный итоговый Document;
- добавить canary в неизвестное top-level raw поле и доказать отсутствие в redacted export и preview.

### P0. ProfileStore и SecretStore всё ещё не образуют одну атомарную транзакцию

Этап C3 предлагает порядок `generation -> active.json -> commit secrets`, а crash между pointer update и secret commit обещает обработать позднее. Но в плане не определён общий durable manifest, связывающий generation, staged secret set и pointer. Recovery SecretStore не знает, нужно ли завершать или откатывать конкретный profile transaction.

Этап E1 с `commit-pending` делает перенос возобновляемым, но не атомарным относительно profile pointer. Он может завершить секреты уже отменённой profile transaction.

Требуется до переключения pointer записать единый durable unit-of-work manifest, содержащий profile ID, generation ID, старый pointer и staged secret generation. Recovery по его state обязан детерминированно завершать либо откатывать **оба** хранилища. Предпочтительнее хранить immutable secret generation и атомарно переключать один manifest/pointer, а не переносить отдельные secret-файлы в общий active.

### P0. `applied-state.json` записывается за пределами commit boundary

План говорит записать AppliedState после успешного `CommitPrepared`, а API затем вызывает `FinalizePrepared`. Если runtime уже заменён, но запись applied-state падает или питание исчезает между этими операциями, runtime и metadata расходятся; если manifest уже `committed`, recovery может не восстановить applied-state.

Исправление:

- applied-state write входит внутрь transaction state machine до финального `TxStateCommitted`;
- checksum записанного runtime проверяется перед commit;
- ошибка записи applied-state после runtime replacement инициирует rollback либо `recovery_required`;
- API не должен самостоятельно собирать последовательность commit/finalize/write-state: один service operation владеет всей транзакцией.

### P0. Rollback сначала переключает head pointer, создавая незащищённое окно

В D4 сначала вызывается `RollbackToGeneration`, затем только начинается prepare/apply. Crash между шагами меняет head, но не runtime и не создаёт manifest. «На ошибке вернуть pointer» также требует заранее сохранённого старого generation ID, чего псевдокод не показывает.

Исправление:

- transaction принимает target generation напрямую, не меняя head заранее;
- snapshot manifest фиксирует old head, old applied и target;
- после успешного runtime verify атомарно обновляются applied state и, если это заданная семантика операции, head pointer;
- сбой на любой границе восстанавливает оба состояния;
- отдельно определить две операции: `restore revision to editor head` и `rollback running runtime`, если UI нуждается в обеих.

## Существенные уточнения

### Secret bindings по строковому индексу хрупкие

Пути вида `inbounds.0.clients.0.uuid` ломаются при перестановке элементов, merge и редактировании. Нужны устойчивые идентификаторы (`inbound tag`, stable client ID/email, outbound tag) либо строго generation-local JSON Pointer, применяемый до любых преобразований. Каждый binding обязан разрешиться ровно один раз; missing/duplicate/type mismatch — hard error, а не компиляция placeholder в runtime.

### `SaveProfile` не должен незаметно мутировать переданный объект

При extraction требуется deep copy до замены значений placeholders. Иначе API response или состояние вызывающего кода может получить изменённый объект. Также `DiskProfileStore` сейчас не владеет `SecretStore`; план должен явно показать dependency injection или вынести сохранение в service-level unit of work.

### Preview semantics не определена до конца

Текущий diff сравнивает `ManagedConfig`, а предложенный pipeline возвращает `Document`. Нужно определить сравнение canonical active runtime Document с candidate Document, нормализацию порядка и обязательную redaction diff output. Просто заменить вызов компилятора недостаточно.

### Containment helper содержит неточную prefix-проверку

`strings.HasPrefix(rel, "..")` отклоняет безопасный дочерний компонент вроде `..safe`. При текущем строгом ID это не проявится, но generic helper должен проверять `rel == ".."` или prefix `".." + separator`. Ошибки `filepath.Abs` нельзя игнорировать. Проверка symlink/reparse components должна быть отдельной platform-aware функцией.

### Acceptance rollback-сценарий не тестирует rollback работающей gen2

В сценарии gen1 остаётся applied, затем создаётся gen2 и выполняется rollback на gen1. Runtime и так уже gen1, поэтому тест слабый. Правильная последовательность:

`apply gen1 -> save gen2 -> apply gen2 -> verify runtime gen2 -> rollback runtime to gen1 -> verify runtime/checksum/applied state gen1`.

### Crash simulation с удалением manifest некорректна

Удалить manifest — не реалистичная проверка границы транзакции: без журнала recovery не знает, что восстанавливать. Нужны injectable failpoints после каждого durable шага и повторное создание Service на том же dataDir. Проверяются prepared/tested/backup_ready/active_replaced/restarting/verifying/applied-state-written/committed.

### Frontend API types тоже входят в контракт

Хотя UI не меняется, удаление `raw_overlay` из response DTO требует обновить `frontend/src/lib/types/xray.ts`, `clientServers.ts`/`clientXray.ts` и их contract tests. Иначе TypeScript продолжит обещать поле, которого больше нет.

### Capability fallback

При неизвестном version output результат должен иметь `Known:false`; при невозможности запустить бинарник endpoint может вернуть структурированный unavailable/error вместе с `Known:false`. Нельзя превращать execution error в успешное обнаружение только с нулевыми флагами без причины.

## Обязательные изменения перед одобрением

1. Добавить extraction/sanitization секретов из raw/base document и disk-wide canary test.
2. Переделать redacted pipeline так, чтобы raw layer и bindings гарантированно не раскрывались.
3. Описать единый durable transaction manifest для profile generation + secret generation.
4. Перенести applied-state update внутрь commit boundary существующей state machine.
5. Переделать rollback: target generation применяется без предварительного pointer switch.
6. Сделать bindings устойчивыми и fail-closed при невозможности разрешения.
7. Исправить acceptance: реально применить gen2 перед rollback; заменить delete-manifest на boundary failpoints.
8. Добавить изменения frontend response types и contract tests.
9. Исправить детали containment (`Abs` errors, component prefix, reparse/symlink handling).

## После внесения этих правок

План можно запускать по этапам A-F. После каждого этапа нужен отдельный targeted test report; переход к следующему этапу разрешён только при зелёных тестах предыдущего. Финальный статус Phase 0/1 не ставить до полного acceptance gate.

## Сообщение агенту

> План v2 стал значительно лучше, но пока не одобрен. Доработайте его по `XRAY_CRITICAL_FIX_PLAN_V2_REVIEW_2026-09-11.md`. Главные блокеры: plaintext secrets остаются в raw.json; redacted pipeline может подмешать их обратно; ProfileStore и SecretStore не имеют общего durable commit; applied-state записывается вне commit boundary; rollback меняет pointer до появления transaction manifest. Исправьте также binding stability и acceptance-сценарий. После обновления укажите новый SHA-256. Реализацию, IPK и deployment пока не начинать.

