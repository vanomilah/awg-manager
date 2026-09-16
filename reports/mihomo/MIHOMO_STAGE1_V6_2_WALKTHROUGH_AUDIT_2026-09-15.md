# Аудит реализации Mihomo Stage 1 Remediation v6.2

Дата: 2026-09-15  
Проверенный отчёт: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`  
Рабочая копия: `E:\AWGM\awg-manager`

## Вердикт

Walkthrough **не принят**. Основная логика exact-set удаления и DNS hardening реализована, существующие тесты проходят, но closure v6.2 заявлен преждевременно. В bootstrap отсутствует заявленная строгая валидация manifest и сохранение quarantine, version matcher остаётся обходным, verifier не реализует полный контракт terminal events, а OpenAPI не содержит обещанный `minLength` для элементов `ids`.

Независимо подтверждено в этой проверке:

- `go test ./internal/mihomo ./internal/mihomonative ./internal/singbox/router ./internal/api` — exit 0;
- `scripts/tests/test-run-acceptance-verifier.sh` — exit 0 для имеющихся 5 сценариев;
- `scripts/tests/verify-openapi-unsupported-rules.py` — exit 0, но сам verifier неполный;
- `scripts/tests/test-bootstrap-acceptance.sh` — exit 0 для имеющихся 7 сценариев;
- `scripts/run-mihomo-acceptance.sh` из `/tmp` — exit 0, реально выполнен `TestGenerateMihomoConfig_RepresentativeBinaryValidation`.

Прохождение этих тестов не закрывает пропущенные случаи ниже.

## Найденные проблемы

### P1 — manifest parser не валидирует данные и допускает выход из cache/staging

Walkthrough заявляет schema/allowlist validation, но `scripts/bootstrap-mihomo-acceptance.sh:27-45` только читает JSON и выводит 11 строк. Не проверяются:

- формат `version`;
- формат SHA-256;
- basename `assetName`;
- наличие `/`, `\\`, `..` и управляющих символов;
- схема URL;
- обязательность HTTPS и test-only разрешение HTTP.

Значения сразу используются в путях:

- `VERSION` в `$CACHE_ROOT/$VERSION/$MANIFEST_SHA` (`bootstrap...:47-48`);
- `BIN_ASSET_NAME` в destination curl/gzip (`bootstrap...:81-94`).

Manifest с `assetName: "../../target"` способен направить запись curl за пределы staging. `version` с path components способен вывести `CACHE_DIR` из ожидаемой version-папки. Это нарушает fail-closed и filesystem boundary.

Исправление:

1. Реализовать все validation rules из v6.2 до построения путей.
2. Проверять `os.path.basename(asset) == asset`, запрещать `.`/`..`, slash/backslash, NUL/control chars.
3. Проверять version как допустимый одиночный directory component.
4. Проверять все hashes по `^[0-9a-fA-F]{64}$`.
5. Production URLs — только HTTPS; HTTP — только при `ACCEPTANCE_ALLOW_HTTP=1`.
6. Добавить offline negative tests path traversal, invalid hash, missing field, non-string field, HTTP without flag и malformed JSON; убедиться, что вне test cache ничего не создано.

### P1 — version matcher можно обмануть посторонним номером версии

`mihomoVersionRegex` ищет любой version-like token во всей строке (`service_mihomo_test.go:872-882`) и возвращает true, если хотя бы один token совпал. Token не привязан к слову `Mihomo`; конфликтующие token не отклоняются.

Пример ложного успеха:

```text
output:   Mihomo v1.18.0 built-with component 1.19.29
expected: v1.19.29
```

Matcher найдёт `1.18.0` и `1.19.29`, после чего примет второй. Это прямо противоречит v6.2: требовался ровно один Mihomo version token и запрет конфликтующих значений. Build metadata `+build.7` также не поддерживается текущим regex.

Исправление:

- извлекать token только из поддерживаемого префикса `Mihomo [Meta] v...`;
- требовать ровно один такой token;
- выполнять полное равенство после удаления одного ведущего `v`;
- поддержать ожидаемые prerelease/build формы либо честно ограничить grammar;
- добавить negative tests conflicting tokens, unrelated matching token, missing token, `1.19`, `1.19.29.1`, leading-zero cases.

### P1 — bootstrap автоматически уничтожает quarantine

После успешной публикации код выполняет `rm -rf "$QUARANTINE_DIR"` (`scripts/bootstrap-mihomo-acceptance.sh:127-131`). Это нарушает явный запрет финального approval: quarantine не должен автоматически удаляться и должен оставаться для диагностики/восстановления.

Текущий Scenario 3 проверяет только, что новый binary исправлен, но не проверяет существование и содержимое quarantine. Поэтому регрессия остаётся незамеченной.

Исправление:

- убрать автоматический `rm -rf` quarantine;
- использовать collision-safe имя с timestamp/PID;
- test должен подтвердить наличие ровно одного quarantine и исходное повреждённое содержимое в нём;
- политика очистки quarantine должна быть отдельной явной пользовательской/CI операцией, не bootstrap side effect.

### P1 — staging создаётся предсказуемо и удаляется через `rm -rf`, а не `mktemp -d`

Вместо согласованного `mktemp -d -p "$CACHE_ROOT" staging.XXXXXX` используется `$CACHE_ROOT/staging.${MANIFEST_SHA}.$$`, затем безусловный `rm -rf` (`bootstrap...:76-79`). Это расходится с approved design и создаёт ненужную поверхность race/pre-creation.

Исправление: создавать staging только через `mktemp -d` внутри canonicalized `CACHE_ROOT`; trap должен удалять только возвращённый `mktemp` путь после проверки его принадлежности test/cache root.

### P1 — verifier не требует ровно одного terminal event

`verify-mihomo-acceptance-json.py` проверяет наличие `run/pass` и отсутствие `skip/fail`, но не число/порядок terminal events (`lines 23-57`). Поток с двумя target `pass` принимается. Self-test содержит лишь пять сценариев и не включает обязательные:

- empty stream;
- malformed-only stream;
- pass without run;
- duplicate terminal pass;
- terminal before run;
- conflicting/duplicate target execution.

Исправление: реализовать state machine либо явную проверку одного `run` и ровно одного terminal action после него. Self-tests должны вызывать тот же verifier и покрыть полный negative corpus.

### P1 — walkthrough приводит вывод не того acceptance test

В walkthrough показан `TestMihomoUpstreamAcceptance_LiveBinary`, но фактический runner запускает и проверяет `TestGenerateMihomoConfig_RepresentativeBinaryValidation` (`scripts/run-mihomo-acceptance.sh:35-41`). Независимый запуск подтвердил именно второе имя.

Это делает приведённое доказательство недостоверным, даже несмотря на то, что реальный текущий runner завершился успешно. Walkthrough нужно обновить фактическим необработанным выводом текущей команды и её exit code.

### P2 — OpenAPI item `minLength` отсутствует, verifier это не проверяет

Сгенерированный schema содержит:

```yaml
ids:
  items:
    type: string
  minItems: 1
  uniqueItems: true
```

У `items` нет `minLength: 1`; `minLength` присутствует только у `revision` (`internal/openapi/swagger.yaml:2592-2603`). При этом `verify-openapi-unsupported-rules.py` проверяет тип items, но не `items.minLength`, и потому даёт ложный PASS.

Исправление:

1. Добиться генерации `items.minLength: 1` устойчивым source-of-truth способом.
2. Добавить assertion `items.get("minLength") == 1` для обоих YAML.
3. Verifier должен проверять и `internal/openapi/swagger.yaml`, и `frontend/static/openapi.yaml`; сейчас читается только первый.

### P2 — disk persistence не проверяется после каждого rejected destructive запроса

Walkthrough утверждает reload verification на всех destructive error paths. На деле store-тесты выполняют duplicate/empty проверки, но reload делается после успешного удаления. API-тест также делает серию ошибок и лишь затем успешное удаление с финальным reload. Это не доказывает неизменность диска отдельно после каждого error case.

Исправление: table-driven subtests с новой store fixture на каждый случай; после каждого rejected request создать новый `Store` из того же файла и сравнить полный persisted snapshot, включая `Enabled`, payload, outbound и порядок.

### P2 — отсутствуют обещанные проверки tool availability и publication failure

Bootstrap не выполняет явные fail-closed проверки `python3`, `sha256sum`, `curl`, `gzip`, `mktemp`, `flock`. Тесты также не покрывают ошибку финального `mv` после quarantine и не проверяют путь восстановления/диагностическое сообщение.

Добавить dependency preflight и fault-injection seam для filesystem publication, затем проверить сохранение quarantine и отсутствие частичного target.

## Что реализовано корректно

- Duplicate ID больше не проходит exact-set comparison.
- Empty selection и empty snapshot отклоняются store.
- Handler различает empty/duplicate/set mismatch и stale revision.
- DNS parser отклоняет `system://...` payload и известные empty-port формы.
- Runner выполняет `cd "$REPO_ROOT"`.
- Cache hit повторно проверяет marker и digests внутри lock.
- Offline tests действительно используют временный `MIHOMO_ACCEPTANCE_CACHE_ROOT` и текущим набором проходят.
- OpenAPI содержит endpoint, required fields, `minItems` и `uniqueItems`.

## Минимальный порядок до закрытия Stage 1

1. Закрыть manifest path/URL/hash validation и negative corpus.
2. Исправить Mihomo-anchored exact version extraction.
3. Перестать удалять quarantine; перейти на `mktemp -d` staging.
4. Сделать verifier stateful и расширить self-tests.
5. Исправить OpenAPI item `minLength` и двухфайловый verifier.
6. Добавить per-error restart persistence и publication-failure tests.
7. Повторить полный набор Go/race/script/OpenAPI/frontend/acceptance проверок.
8. Выпустить новый walkthrough только с фактическими командами и выводом текущей реализации.

## Границы аудита

IPK не собирался, деплой не выполнялся, `--force-reinstall` и cleanup не запускались. Offline bootstrap работал только в собственном временном cache. Production acceptance использовал уже валидный существующий cache и не скачивал артефакты.
