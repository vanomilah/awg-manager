# Повторное ревью Mihomo Stage 2 Remediation Plan v9

**Дата:** 2026-09-15  
**Документ:** `implementation_plan.md` — *Mihomo Stage 2 Remediation Plan (Revised v9)*  
**Предыдущее ревью:** `MIHOMO_STAGE2_REMEDIATION_PLAN_REVIEW_2026-09-15.md`  
**Рабочая копия:** `E:\AWGM\awg-manager`, ветка `feature/mihomo-ai-proxyrt`

## Вердикт

**v9 существенно доработан и почти готов к передаче в реализацию, но запускать весь план буквально пока рано. Осталось закрыть 3 критичных протокольных неоднозначности и 7 точечных замечаний.**

Статус: `APPROVE AFTER REQUIRED AMENDMENTS`.

В отличие от предыдущей версии, v9 действительно включает:

- разделение process identity и daemon epoch;
- mode-aware listener derivation;
- exact bridge API;
- version vector для compile input;
- перечень pending-input producers;
- global mutation gate и recovery allowlist;
- generation bundles вместо одного LKG-файла;
- allowlisted evidence DTO;
- таблицу из 64 сценариев;
- четыре последовательных execution gate.

Это устраняет большую часть замечаний предыдущего ревью. Ниже перечислены оставшиеся изменения, которые должны быть внесены в сам план до кодинга.

## P0 — оставшиеся блокирующие неоднозначности

### P0-1. Generation bundle может получить store уже следующего поколения

В текущем state flow store мутируется в `StateStoreMutated`, а LKG bundle создаётся позже при переходе `StateCandidateValid -> StateLKGSecured`. Если в этот момент взять новый `store.snapshot.json`, он уже будет содержать target desired state, тогда как архивируемый `config.yaml` относится к предыдущему active generation.

Такой bundle внутренне несогласован: rollback восстановит старый config вместе с новым store.

**Обязательная поправка:**

- snapshot, созданный **до mutation** в `StateSnapshotSecured`, является store-состоянием предыдущего applied generation;
- именно этот immutable pre-mutation snapshot включается в bundle предыдущего поколения;
- `generation.manifest.json` хранит и проверяет согласованную пару `AppliedStoreDigest + AppliedConfigDigest + AppliedInputDigest` из прежнего `verified-active.json`;
- перед созданием bundle coordinator доказывает, что digest pre-mutation snapshot совпадает с прежним `AppliedStoreDigest`;
- если совпадение доказать нельзя, LKG не обновляется и apply переходит в `recovery_required`;
- для apply без store mutation текущий applied store копируется в bundle только после проверки его digest;
- после успешного commit target store становится материалом для будущего bundle, но не LKG текущей незавершённой транзакции.

Нужно также описать порядок durable publication bundle: staging directory на том же filesystem, fsync каждого файла, fsync staging dir, atomic rename в `generations/<id>`, fsync `generations/`, затем atomic write + fsync `lkg.pointer.json` и parent directory. Набор из нескольких файлов нельзя называть атомарным без этого протокола.

### P0-2. Version vector без writer-side протокола не гарантирует stable snapshot

Алгоритм `v1 -> read sources -> v2` работает только если каждый writer изменяет revision **до и после** критической секции либо держит общий lock. Если writer сначала последовательно пишет несколько файлов и лишь в конце увеличивает revision, reader может увидеть одинаковые `v1 == v2`, но собрать промежуточную смесь.

**Обязательная поправка:** определить writer-side seqlock/barrier:

- у каждого источника revision становится нечётным до первой mutation и чётным после durable завершения всех связанных записей;
- reader не начинает snapshot при нечётной revision;
- snapshot принимается только если before/after revisions одинаковы и чётны;
- alternatively каждый source предоставляет `Snapshot() (payload, revision)` под собственным lock, а cross-source coordinator повторяет чтение до стабильного vector;
- источники без revision используют digest стабильного immutable файла, но writer обязан публиковать файл atomic rename-операцией;
- план должен перечислить конкретные изменения в `SettingsStore`, orchestrator, router config store, native store и tunnel/AWG catalogs, а не ограничиваться `service_mihomo.go`;
- retries должны уважать `context.Context`; после исчерпания возвращается typed `ErrInputSnapshotConflict`.

Без writer-side изменений заявленная «eliminating generation skew» гарантия неверна.

### P0-3. `input.pending.json` всё ещё имеет crash-window

Формулировка «before or during the mutation boundary» недопустима. Если desired state уже изменён, а marker ещё не записан, авария снова теряет convergence request.

При этом final `TargetInputDigest` часто нельзя вычислить до mutation. Нужен двухфазный intent protocol:

1. durable записать `PendingInputRecord{state: intent, source, base_version_vector, operation_id}` **до** изменения desired state;
2. выполнить mutation;
3. собрать стабильный target snapshot;
4. durable обновить marker до `state: pending` с `target_version_vector` и `target_input_digest`;
5. выполнить apply;
6. удалить marker только после durable verified-active с совпадающим digest.

При старте:

- `intent` означает, что итог mutation неизвестен; coordinator перечитывает actual desired state и создаёт target digest;
- `pending/converging` сходятся идемпотентно;
- ошибка записи intent не позволяет начинать mutation;
- ошибка финализации marker после mutation оставляет durable intent, а не немаркированное изменение;
- параллельные producers либо сериализуются, либо marker поддерживает очередь/coalesced target generation. Один файл нельзя молча перезаписывать конкурентным событием.

Текущие состояния только `pending/converging` следует расширить либо заменить единым durable change journal.

## P1 — точечные обязательные уточнения

### P1-1. Commit boundary противоречит сценарию 34

Transition table говорит, что failure при `StatePublished -> StateCommitted` переводит систему в recovery_required. Сценарий 34 говорит, что после durable `verified-active.json`, но ошибки записи committed manifest startup автоматически завершает commit.

Нужно разделить две границы:

- failure **до** durable verified-active: commit не состоялся, recovery/rollback согласно observed state;
- verified-active durable и полностью совпадает с manifest target: это commit point; failure последующей записи manifest означает roll-forward/cleanup-pending, а не откат;
- mismatch или невозможность доказательства означает `recovery_required`.

Authoritative truth и порядок сравнения должны быть указаны однозначно.

### P1-2. Process identity требует проверки cmdline и controller socket owner

`ConfigDir` нельзя получить из `/proc/<pid>/stat` или `/proc/<pid>/exe`. Его нужно доказать через `/proc/<pid>/cmdline` (`-d <expected-dir>`) с корректным разбором NUL-separated arguments.

Controller `/version` сам по себе не доказывает, что отвечает проверяемый PID. Controller TCP listener также должен пройти inode ownership check для того же process identity. Payload version — дополнительная, а не основная гарантия.

Operator generation является in-memory счётчиком и после рестарта AWG Manager не доказывает identity ранее запущенного процесса. Его следует использовать только внутри текущего daemon epoch; persisted proof опирается на PID + proc start ticks + executable + cmdline + socket ownership.

### P1-3. ListenerSpec должен описывать точный bind, а не только порт

План упоминает compiled AST, но интерфейс `ListenerSpec` не обновлён. Требуются как минимум:

```go
type ListenerSpec struct {
    Network  string // tcp or udp
    Family   string // ipv4, ipv6, any
    Address  string
    Port     uint16
    Purpose  string
}
```

Проверка только порта даст ложный успех, если Mihomo слушает другой address/family. Набор должен выводиться из typed config до YAML serialization. Sidecar mixed listeners проверяются по реально заданным network capabilities; `RuntimeOff` возвращает пустой набор.

Добавление `InboundPort` в `BridgeRef` само по себе не нужно, если authoritative listener уже присутствует в compile result. Следует выбрать один источник истины, чтобы port не расходился в двух структурах.

### P1-4. ExactBridgeRuntime всё ещё требует postcondition для withdraw

`WithdrawBridge` возвращает только error. Для симметричной доказуемости он должен вернуть observed result либо coordinator обязан вызвать `InspectBridge` и доказать `Exists == false`. `PublishBridge` также считается завершённым только после независимого inspect, а не только по возвращаемому объекту той же операции.

Нужно определить identity bridge (ProxyIndex недостаточно без owner/generation) и поведение при already-present, чужом owner и частичном NDMS/kernel состоянии.

### P1-5. Evidence DTO всё ещё содержит потенциально секретный текст

`FailureReason` и `RecoveryMarkerText` являются произвольными строками и могут включать URL, заголовки, credentials или фрагмент config. Это нарушает собственное правило allowlist.

Заменить их на:

- enumerated `FailureCode`;
- allowlisted component/stage;
- sanitized human summary, создаваемый из кода, а не исходного error text;
- hashes/counts вместо raw values.

Canary-тест должен искать plaintext, URL-encoded, base64 и JSON-escaped варианты секретов. Raw error chain и logs в evidence не включать.

### P1-6. `repair_binary` не специфицирован

Action добавлен в allowlist, но отсутствуют его state machine, authorization, source validation и postconditions. Нужно либо удалить `repair_binary` из v9, либо описать:

- кто и откуда устанавливает binary;
- signature/checksum/architecture validation;
- что происходит с работающим процессом;
- обязательный `mihomo -t`, controlled restart и full readiness proof;
- почему операция разрешена в degraded mode;
- rollback binary при failure.

Обычные install/update endpoints при этом остаются заблокированы.

### P1-7. «64 сценария» пока не является полной матрицей границ

Таблица теперь реально содержит 64 строки — это хороший прогресс. Но число 64 не делает её исчерпывающей. В ней отсутствуют либо недостаточно явно выделены:

- staged bundle file/dir fsync и `lkg.pointer.json` failures;
- pre-mutation snapshot digest mismatch;
- writer revision odd/in-progress и одинаковый vector при non-bracketed writer;
- crash до intent, после intent, после desired mutation и до target digest;
- concurrent pending producers/coalescing;
- executable/cmdline mismatch;
- `/proc/<pid>/stat` с необычным comm;
- controller owned by alien PID;
- IPv4/IPv6/address mismatch;
- sidecar-only, TUN и RuntimeOff listener sets;
- withdraw postcondition и foreign bridge ownership;
- failure удаления manifest и `lkg.pointer` cleanup;
- API coverage для router/tunnel/background gate, а не только native/settings/install;
- `ErrInputSnapshotConflict` после исчерпания retries;
- cancellation после commit point и detached rollback/recovery.

Не обязательно сохранять ровно 64. Правильнее назвать раздел `Traceable Fault-Injection Matrix` и добавить все границы. Каждая строка должна иметь test function name и Gate, иначе команда `-run 'TestCoordinator_Scenario'` не доказывает связь таблицы с кодом.

## Дополнительные замечания к execution gates

1. Gate 1 не должен объявляться принятым без тестов partial generation bundle и pointer publication.
2. Gate 2 должен включать production Linux probe integration test, а не только fake `/proc` unit tests.
3. Gate 3 должен менять writer-side stores; одной правки `AssembleCompileInput` недостаточно.
4. Gate 3 должен включать все pending producer callbacks и проверку concurrent changes.
5. Gate 4 не должен откладывать удаление direct writer до самого конца: после появления нового coordinator path старый writer следует удалить до интеграционных тестов, иначе два пути продолжают расходиться.
6. `git grep` остаётся диагностической командой, а не автоматическим тестом. Нужен script/test с allowlist и ненулевым exit code.
7. Проверка должна включать `go test ./...` или честно перечислять исключённые пакеты; текущая команда не включает `internal/api` и `cmd/awg-manager`, хотя именно там меняются gate/wiring/API.
8. Frontend проверяется после regeneration OpenAPI и отдельным тестом degraded workflow.

## Финальный список изменений в implementation_plan.md

Перед запуском агент должен внести в v9 следующие правки:

1. Явно связать LKG bundle с **pre-mutation** store snapshot предыдущего verified generation.
2. Добавить staged-directory + fsync + atomic pointer publication protocol.
3. Добавить writer-side seqlock/atomic snapshot contract каждому источнику version vector.
4. Заменить `before or during` marker на durable two-phase intent-before-mutation protocol.
5. Развести commit point и post-commit manifest/cleanup failure.
6. Дополнить process proof проверкой `/proc/<pid>/cmdline` и ownership controller socket.
7. Сделать ListenerSpec address/family-aware и убрать дублирование bridge port.
8. Доказать postcondition каждого bridge publish/withdraw и owner identity.
9. Удалить произвольные текстовые поля из evidence DTO.
10. Удалить или полностью специфицировать `repair_binary`.
11. Расширить матрицу недостающими сценариями и связать строки с именами тестов.
12. Включить `internal/api`, `cmd/awg-manager` и общий `go test ./...` в финальную проверку.

## Итог для пользователя и следующего агента

План v9 — **правильная архитектурная основа и большой шаг вперёд**. Его не нужно переписывать заново. После двенадцати уточнений выше можно запускать Gate 1 в работу.

Не следует сразу реализовывать все четыре gate одним большим изменением. После каждого gate нужен отдельный walkthrough с:

- перечнем фактически изменённых файлов;
- mapping «пункт плана -> тест»;
- результатами команд без пересказа;
- списком оставшихся ограничений;
- запретом переходить к следующему gate, пока текущие postconditions не доказаны.
