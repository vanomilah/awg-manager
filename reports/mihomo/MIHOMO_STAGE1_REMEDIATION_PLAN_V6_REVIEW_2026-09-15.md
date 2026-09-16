# Ревью Remediation Plan v6: Mihomo Stage 1 Closure

Дата: 2026-09-15  
Проверенный план: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Основание: `MIHOMO_STAGE1_FINAL_REMEDIATION_WALKTHROUGH_AUDIT_2026-09-15.md`

## Вердикт

План **почти готов**, но запускать его как окончательный план закрытия Stage 1 пока рано. Он правильно охватывает все найденные P0/P1/P2, однако требует нескольких обязательных уточнений. Без них можно получить ложную приёмку bootstrap-кода, не получить заявленные OpenAPI-ограничения и оставить acceptance runner с неполной машинной проверкой.

После внесения поправок ниже план можно отдавать в реализацию.

## Что в плане сделано правильно

- Исправление exact-set contract и запрет duplicate/empty IDs сформулированы корректно.
- Защитная проверка предусмотрена и в HTTP handler, и в store.
- Предусмотрена проверка неизменности persisted data после ошибочных destructive-запросов.
- DNS fail-closed случаи `system://...` и bracketed IPv6 с пустым портом включены.
- Убраны `eval` и безусловный `rm -rf "$CACHE_DIR"`.
- Предусмотрены lock, повторная проверка cache внутри lock, staging и quarantine.
- Runner должен стать независимым от cwd.
- Version matching переводится с substring на точное сравнение.
- Основные Go, race, frontend, OpenAPI и whitespace проверки перечислены.

## Обязательные изменения перед запуском

### 1. P1 — нельзя тестировать shell bootstrap из `service_mihomo_test.go`

План предлагает добавить `TestValidateMihomoAcceptanceFixtures_CorruptedCacheQuarantine` в `internal/singbox/router/service_mihomo_test.go`. Этот Go-файл тестирует fixture validator и запуск Mihomo, но не вызывает и не контролирует `bootstrap-mihomo-acceptance.sh`. Такой тест не докажет quarantine, locking, atomic publication или отсутствие повторной загрузки.

Требуется отдельный integration-test для shell bootstrap, например:

- `scripts/tests/bootstrap-mihomo-acceptance_test.sh`; либо
- Go integration-test в отдельном пакете, который запускает скрипт как subprocess с полностью локальным fake manifest и локальным HTTP server.

Тест не должен обращаться в интернет. Он должен создавать маленькие gzip/geodata fixtures, вычислять их SHA-256 и проверять:

1. первый запуск публикует валидный cache;
2. второй запуск не скачивает файлы повторно;
3. повреждённый target переносится в quarantine, а валидный новый cache публикуется;
4. два одновременных процесса завершаются успешно и оставляют ровно один целый target;
5. ни один процесс не видит частично опубликованный cache;
6. невалидный checksum не заменяет существующий валидный target;
7. ошибка загрузки оставляет target неизменным, staging очищается.

### 2. P1 — безопасный способ чтения manifest должен быть указан точно

Фраза «Python emitting tab-separated key-value stream, read using `read -r`» недостаточна. Обычный `while ... | read` выполняется в subshell, поэтому переменные после цикла могут оказаться пустыми. TSV также ломается на tab/newline в значениях.

Зафиксировать реализацию:

- Python валидирует обязательную схему и печатает значения в строго заданном порядке, разделённые NUL;
- Bash читает их через `mapfile -d '' -t manifest_values < <(python3 ... "$MANIFEST")` либо последовательные `IFS= read -r -d ''` из process substitution;
- путь manifest передаётся как `sys.argv[1]`, а не встраивается в Python source;
- проверяется точное число полей;
- `version`, hashes, asset filename и URLs проходят allowlist-валидацию;
- asset filename обязан быть basename без `/`, `\`, `..` и управляющих символов;
- checksum обязан соответствовать `^[0-9a-fA-F]{64}$`;
- URL разрешён только ожидаемой схемы (для production manifest — HTTPS; локальный test server допускается только явным test-флагом).

### 3. P1 — lock должен иметь чёткую область и гарантированный fallback

Указать, что lock берётся **до** второй проверки cache и удерживается до завершения публикации/quarantine. Глобальный `.bootstrap.lock` безопасен, но без необходимости сериализует разные версии; предпочтительнее lock, вычисленный по manifest SHA, вне target directory.

Также нужно определить поведение при отсутствии `flock`. Нельзя молча продолжать без блокировки. Допустимы:

- fail-closed с понятной ошибкой зависимости; либо
- атомарный lock-directory с bounded retry и cleanup только собственного lock владельца.

Quarantine name должен быть collision-safe (`timestamp + PID` или `mktemp`-основанное имя). Автоматическое удаление quarantine в рамках bootstrap запрещено.

### 4. P1 — OpenAPI ограничения нельзя оставить только в комментарии

Изменение текста `@Param` само по себе не гарантирует генерацию:

```yaml
minItems: 1
uniqueItems: true
items:
  type: string
  minLength: 1
```

План должен требовать проверить именно итоговый `internal/openapi/swagger.yaml` и `frontend/static/openapi.yaml`. Если используемый swag generator не выводит эти ограничения из Go-тегов, нужно либо применить поддерживаемые им schema tags, либо внести ограничение через поддерживаемый механизм генерации, не ручную правку, которая исчезнет при следующем `go generate`.

Добавить acceptance assertion, читающий сгенерированный YAML и проверяющий `minItems`, `uniqueItems`, `minLength` у конкретного DTO.

### 5. P1 — machine verifier должен требовать событие `run`

План исправляет cwd, но не дополняет проверку JSON событий. Runner сейчас проверяет наличие test events, отсутствие `skip` и наличие `pass`. Требуется также явно подтвердить, что для целевого теста было событие `run`, а package-level pass не подменяет результат теста.

Ожидаемый контракт:

- target emitted `run`;
- target emitted `pass`;
- target emitted no `skip` and no `fail`;
- `go test` exit code равен 0;
- JSON parser сам завершился кодом 0.

Добавить self-tests verifier-а на empty output, malformed-only output, skip, fail, pass-without-run и корректный run+pass.

### 6. P1 — destructive API тесты должны проверять restart persistence

Формулировки «persisted data untouched» недостаточно. После duplicate/empty/missing/null запросов тест должен:

1. проверить состояние текущего Store;
2. создать новый Store из того же файла;
3. подтвердить, что оба исходных правила сохранились с теми же полями и состоянием `Enabled`.

Это исключит ситуацию, когда rollback исправил память, но диск уже повреждён.

### 7. P2 — уточнить version regex и запретить частичное совпадение prerelease

Предложенный regex может быть слишком узким для SemVer metadata и слишком зависимым от конкретной строки вывода. Требуется:

- извлечь ровно один version token из поддерживаемых форм вывода Mihomo;
- нормализовать только ведущий `v`;
- сравнивать всю строку версии, включая prerelease/build metadata, если она присутствует в expected manifest;
- отклонять отсутствие token и несколько противоречивых token;
- тесты: exact stable, exact prerelease, near-match, missing token, extra numeric prefix/suffix.

### 8. P2 — добавить проверку cleanup staging и целостности marker

`.bootstrap_complete` нельзя считать достаточным сам по себе. План уже предусматривает digest recheck, это нужно сохранить как обязательный контракт после lock. Marker создаётся последним в staging; target считается готовым только при наличии marker и совпадении всех digest.

Integration-tests должны проверять, что после успеха и после каждой fault injection не осталось `staging.*`, созданных данным процессом. Чужой staging удалять нельзя.

## Исправленный порядок выполнения

1. Store exact-set/duplicate/empty fix и restart-persistence tests.
2. Handler validation и HTTP destructive tests.
3. DNS parser и negative corpus.
4. Exact version parser и unit tests.
5. Bootstrap безопасный parser + scoped lock + quarantine + atomic publication.
6. Отдельный offline integration harness для bootstrap и конкурентных запусков.
7. Runner cwd fix + run/pass/no-skip verifier и его self-tests.
8. OpenAPI generation с машинной проверкой фактических array constraints.
9. Полная повторная верификация.

## Итоговые критерии приёмки

Помимо команд исходного плана обязательны:

```text
Store duplicate test: [A,B] vs [A,A] -> ErrSelectionMismatch; memory and reloaded disk unchanged
API empty/missing/null ids -> 400 INVALID_REQUEST
API duplicate ids -> 400 SELECTION_MISMATCH
OpenAPI generated schema -> minItems=1, uniqueItems=true, item minLength=1
Bootstrap two-process race -> both successful, one valid target, no partial target
Bootstrap checksum/download failure -> valid target unchanged
Bootstrap corrupted target -> unique quarantine + valid replacement
Runner invoked from /tmp -> target emits run and pass, no skip/fail
DNS malformed corpus -> all rejected
Version near-match/missing/ambiguous tokens -> rejected
```

После этого выполнить исходный набор Go/race/frontend/OpenAPI/diff проверок. IPK и деплой не являются частью Stage 1 closure и не должны выполняться в этом плане.

## Решение для владельца

**Не запускать текущий v6 без правок.** Вернуть агенту этот файл и попросить выпустить v6.1 с восемью уточнениями выше. После v6.1 достаточно короткого повторного ревью; архитектурной переработки плана больше не требуется.
