# Audit: Mihomo Stage 1 v6.6 Walkthrough

Дата: 2026-09-15  
Проверенный отчёт: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`  
Рабочее дерево: `E:\AWGM\awg-manager`

## Вердикт

**Реализация в основном работоспособна, но заявление walkthrough о полном закрытии пока не подтверждено.** Независимо перепроверены и прошли manifest tests, 15 bootstrap scenarios, 17 verifier scenarios, затронутые Go packages, OpenAPI verifier и mandatory acceptance с реальным Mihomo binary. При этом найдены два дефекта bootstrap, один отсутствующий обязательный regression check и недостоверный фрагмент «реального вывода» в walkthrough.

Статус: **условно принято после небольшой корректирующей правки**. IPK и деплой не требуются. Не нужно снова проектировать v6.7 целиком — исправить перечисленную дельту, перезапустить проверки и обновить walkthrough фактическим выводом.

## Независимо подтверждено

Следующие команды выполнены на текущем дереве 2026-09-15:

```text
python3 -m unittest scripts/tests/test_validate_manifest.py
Result: 13 tests, OK

bash scripts/tests/test-bootstrap-acceptance.sh
Result: ALL 15 BOOTSTRAP ACCEPTANCE SCENARIOS PASSED SUCCESSFULLY!

bash scripts/tests/test-run-acceptance-verifier.sh
Result: ALL 17 VERIFIER SCENARIOS PASSED SUCCESSFULLY!

go test -run TestValidateMihomo ./internal/singbox/router
Result: PASS

go test -run TestDeleteUnsupportedRules_PerErrorPersistence ./internal/mihomonative
Result: PASS

python3 scripts/tests/verify-openapi-unsupported-rules.py
Result: SUCCESS, including items.minLength: 1

bash -n <affected shell scripts>
python3 -m py_compile <affected Python scripts>
Result: PASS

go test -count=1 ./internal/mihomonative ./internal/mihomo ./internal/api ./internal/singbox/router
Result: all four packages PASS

bash scripts/run-mihomo-acceptance.sh
Result: TestGenerateMihomoConfig_RepresentativeBinaryValidation and package lifecycle PASS
```

Таким образом, основной acceptance path не является ложным: текущий настоящий бинарник Mihomo проходит заявленную проверку.

## Найденные проблемы

### P1. Preflight не содержит `rm`, хотя bootstrap использует его

`scripts/bootstrap-mihomo-acceptance.sh` объявляет `REQUIRED_CMDS`, но пропускает `rm`. При этом `rm` используется как минимум для:

- удаления временного fields-файла;
- очистки staging через `rm -rf`;
- удаления сжатого бинарника после распаковки.

Заявление «definitive list of all external commands» неверно. Scenario 15 тоже не проверяет отсутствие `rm`, поэтому harness проходит.

Исправление:

1. Добавить `rm` в `REQUIRED_CMDS`.
2. Получать critical tool cases из полного required list либо добавить `rm` в table-driven набор.
3. Добавить consistency test: каждое external command, используемое production script, учтено в preflight (или сократить production dependencies).

### P1. Signal traps могут завершить прерванный bootstrap кодом 0

Сейчас установлено:

```bash
trap cleanup EXIT INT TERM
```

`cleanup()` сохраняет текущее `$?` и вызывает `exit "$exit_code"`. Для `INT`/`TERM` сохранённый status может быть status предыдущей успешной команды, включая `0`. В результате прерванная загрузка потенциально будет представлена вызывающему процессу как успешная.

Исправление: разделить handlers:

```bash
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
```

EXIT cleanup должен сохранить уже установленный 130/143 и удалить только staging. Добавить acceptance scenario с зависшим mock-download: послать TERM/INT, проверить ненулевой ожидаемый exit, отсутствие staging/target и сохранность прежнего cache.

### P1. Scenario 3 не доказывает заявленную retention policy quarantine

После замены повреждённого cache production code сохраняет старый cache в `quarantine.*`. Scenario 3 проверяет только новый SHA и четыре HTTP-запроса. Он не проверяет:

- что quarantine действительно создан;
- что в нём лежит исходное повреждённое содержимое;
- что bootstrap его автоматически не удалил.

Это было одним из исходных замечаний аудита. Добавить строгие assertions в Scenario 3 с изолированным cache root и `snapshot_tree`.

### P1. Walkthrough содержит вывод, не соответствующий текущему исходнику

В walkthrough раздел 2.6 показывает subtests:

```text
StaleRevision
EmptyIDs
EmptyElement
DuplicateIDs
SubsetIDs
SupersetIDs
DisjointIDs
HappyPathDelete
```

Но текущий `internal/mihomonative/rules_parity_test.go`, изменённый **до создания walkthrough**, содержит другой набор:

```text
nil IDs
empty IDs
empty string in IDs
duplicate IDs
nonexistent ID
stale revision
subset of IDs
superset of IDs
```

В текущей функции отсутствует `HappyPathDelete`. Следовательно, приведённый блок не является фактическим выводом проверяемой редакции. Его нужно удалить и заменить результатом:

```bash
go test -count=1 -v -run '^TestDeleteUnsupportedRules_PerErrorPersistence$' ./internal/mihomonative
```

Walkthrough нельзя считать доказательным документом до исправления этого расхождения.

### P2. Заявления о полном наборе проверок завышены

Walkthrough пишет «full Go test suite», но реально указан только набор из четырёх пакетов, а не `go test ./...`.

Также план требовал frontend lint, но отчёт показывает лишь:

```bash
npx eslint src/lib/api/schemas.gen.ts
```

Это не эквивалентно `npm run lint`, который запускает `eslint .` и присутствует в CI.

Исправление формулировок и проверки:

- назвать Go-прогон «affected package suite» либо отдельно выполнить `go test ./...`;
- выполнить `npm run lint` для полного frontend scope;
- не писать «race on all packages»: приведённый race command не включает `internal/api` и тем более весь module;
- для непроверенных частей честно указать `not run`/`pending`.

### P2. Validator URL contract не полностью проверяет синтаксис authority/port

`urllib.parse.urlparse()` не гарантирует полной валидации URL. Текущая функция не обращается к `p.port`, поэтому значение вроде `https://host:not-a-port/file` может пройти validator и упасть позже в curl.

Добавить явное чтение `p.port` с обработкой `ValueError`, запрет управляющих символов и tests для invalid port/unmatched IPv6 bracket/whitespace-control cases. Это hardening, не блокирующий уже подтверждённый manifest.

### P2. OpenAPI atomic replace не означает crash-durable commit

Patcher корректно пишет temp в том же каталоге, выполняет `fsync` файла и `os.replace`. Это обеспечивает atomic visibility. Directory fsync не выполняется, поэтому не следует называть операцию crash-durable. Текущий walkthrough этого прямо не обещает; достаточно сохранить точную терминологию.

## Что исправлять агенту

1. Добавить `rm` в production preflight и missing-tool tests.
2. Разделить EXIT/INT/TERM traps и добавить signal interruption scenario.
3. Усилить Scenario 3 проверкой сохранённого corrupt quarantine.
4. Перезапустить bootstrap harness; скорректировать число сценариев, если signal test оформлен отдельным номером.
5. Перезапустить точный verbose persistence test и заменить недостоверный блок walkthrough.
6. Выполнить `npm run lint`; корректно назвать affected-package Go/race проверки либо выполнить более широкий scope.
7. Желательно усилить URL validation и его unit tests.

## Повторная приёмка

Обязательный минимальный набор после правок:

```bash
python3 -m unittest scripts/tests/test_validate_manifest.py
bash scripts/tests/test-bootstrap-acceptance.sh
bash scripts/tests/test-run-acceptance-verifier.sh
go test -count=1 ./internal/mihomonative ./internal/mihomo ./internal/api ./internal/singbox/router
go test -count=1 -v -run '^TestDeleteUnsupportedRules_PerErrorPersistence$' ./internal/mihomonative
python3 scripts/tests/verify-openapi-unsupported-rules.py
bash scripts/run-mihomo-acceptance.sh
cd frontend && npm run lint && npm run check -- --threshold warning && npm test && npm run build
git diff --check
```

Если широкий frontend/Go прогон не помещается по времени, walkthrough обязан отметить его как непроверенный, а не как PASS.

## Итог

Функциональное ядро remediation подтверждено и не требует отката. Полное закрытие Stage 1 отклонено только до устранения небольшой дельты выше и исправления недостоверного walkthrough. Сборка IPK и установка на роутер на этом этапе не нужны.
