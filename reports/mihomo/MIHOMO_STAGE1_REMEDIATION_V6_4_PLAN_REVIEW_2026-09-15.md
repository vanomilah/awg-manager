# Review: Mihomo Stage 1 Remediation v6.4

Дата: 2026-09-15  
Проверенный план: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Репозиторий: `E:\AWGM\awg-manager`

## Вердикт

**v6.4 существенно лучше v6.3, но план всё ещё нельзя исполнять буквально.** Архитектурные решения в основном закрыты, однако в приведённом псевдокоде есть несколько детерминированных ошибок. Они приведут к падению корректного bootstrap, потере кода `recovery-required`, ложным или всегда падающим acceptance tests и обходу проверки версии.

Нужна короткая редакция v6.5. После исправления пунктов P0/P1 ниже реализацию можно запускать.

## Что действительно исправлено

- Закрыты пользовательские решения по quarantine, версии и OpenAPI pipeline.
- Manifest contract теперь перечисляет точные вложенные поля и типы.
- Validator вынесен в foreground subprocess.
- Появились формальная модель публикации, failpoint и recovery outcome.
- OpenAPI patcher привязан к `go generate` через wrapper.
- Verifier разделён на test/package state machines.
- Добавлены намерения проверять файл store byte-for-byte после каждого отказа.
- `frontend/static/openapi.yaml` действительно gitignored; корректно, что CI не обязан проверять этот dev-only файл.

## P0 — блокирующие ошибки плана

### 1. Проверка «12-го NUL-поля» неверна и отклонит корректный вывод

В строках 216–220 предлагается:

```bash
tail -c +$(($(wc -c < "$FIELDS_TMP"))) "$FIELDS_TMP"
```

`tail -c +N` начинает вывод **с N-го байта**, а не после него. Для корректного потока последний байт — NUL, поэтому `read -d ''` увидит ещё одну пустую запись и сочтёт её extra output. Кроме того, вычисление byte offset вообще не доказывает, что было прочитано ровно 11 полей.

Исправление: читать весь файл одним `mapfile`:

```bash
mapfile -d '' -t fields < "$FIELDS_TMP"
if [ "${#fields[@]}" -ne 11 ]; then
    die "validator produced ${#fields[@]} fields, expected 11"
fi
```

Затем присвоить 11 переменных по индексам. Validator обязан всегда завершать поток NUL; это отдельно проверяется Python unit test и/или последним байтом файла. Нужны test-only validator fixtures, возвращающие 10, 12 полей и ненулевой хвост.

### 2. Код возврата `publish_cache` теряется

Строки 338–343:

```bash
if ! publish_cache; then
    EXIT_CODE=$?
```

Внутри ветки `$?` — статус логического `!`, то есть `0`, а не исходные `1`/`2`. Ветка `RECOVERY-REQUIRED` никогда не будет распознана вызывающим кодом.

Нужно сохранять status без `!`, безопасно относительно `set -e`, например:

```bash
set +e
publish_cache
rc=$?
set -e
if [ "$rc" -ne 0 ]; then
    [ "$rc" -eq 2 ] && echo "CRITICAL: Recovery failed..." >&2
    exit "$rc"
fi
```

Либо использовать `publish_cache || rc=$?` с предварительным `rc=0`. Acceptance должен различать exit 1 и специальный exit 2.

### 3. Scenario 13 сравнивает разные состояния кеша

План сначала сохраняет snapshot корректного cache, затем портит `mihomo`, после чего failpoint восстанавливает из quarantine уже **испорченный** cache. Сравнение с snapshot до порчи закономерно провалится даже при правильном rollback.

Нужно либо:

- снять snapshot после контролируемой порчи и доказать восстановление именно pre-transaction state;
- либо инициировать refresh без изменения содержимого cache (например, убрать marker после snapshot и включить его в ожидаемое состояние осознанно);
- либо создать отдельный valid-but-stale generation fixture.

Тест должен сравнивать cache непосредственно перед транзакцией с cache после rollback byte-for-byte, включая имена и mode файлов.

### 4. Go matcher снова разрешает ложное совпадение и не выполняет прежнее требование exact-one

Использование `FindStringSubmatch` берёт первое совпадение и молча игнорирует второе. Вывод:

```text
Mihomo v1.18.0 Mihomo v1.19.29
```

не отклоняется как неоднозначный, хотя это было обязательным требованием предыдущего аудита.

Кроме того, regex с `\b` частично сопоставит `Mihomo v1.19.29+build`: boundary существует между `9` и `+`, поэтому заявленный negative test с expected `v1.19.29` фактически может вернуть true.

Исправление:

- извлечь **все** токены после `Mihomo`/поддерживаемого фактического banner;
- потребовать ровно один кандидат;
- валидировать полный токен целиком, не обрезая `+build`;
- отдельно валидировать `expectedVersion` тем же awgm-version контрактом;
- добавить конфликтующий двойной banner и `+build` с expected base в тесты.

Нужно сверить реальный `mihomo -v` banner: v6.4 убрал поддержку `Mihomo Meta`, которая присутствовала в v6.3, без доказательства, что она больше не нужна.

## P1 — обязательные уточнения

### 5. Preflight фактически выполняется не до внешних команд

Текущий bootstrap вычисляет `REPO_ROOT` через `dirname` и `pwd`, затем `MANIFEST_SHA` через `sha256sum | awk` до бывшего parser block. Простая вставка preflight «после set -euo pipefail» требует перестройки порядка, иначе ещё до проверки вызываются непроверенные инструменты.

В список также не включены команды, используемые предложенным кодом: как минимум `dirname`, `rmdir`, `pwd` и shell built-in/external assumptions должны быть классифицированы. Формулировка «все external commands» сейчас неверна.

Сначала допустимо определить путь скрипта средствами shell, затем выполнить минимальный bootstrap preflight, и только после него hash/validator. План должен показать окончательный порядок верхней части файла, а не независимые вставки.

### 6. Recovery-failure test не детерминирован

Scenario 13b предлагает сделать parent read-only. Тесты могут выполняться от root, для которого permission trick не гарантирует отказ. Кроме того, изменение parent способно сломать не только restore, но и более ранний переход.

Добавить отдельный test-only failpoint `_BOOTSTRAP_FAIL_RESTORE=1`, срабатывающий только после fail-publish и до reverse `mv`. Проверить exit 2, точный diagnostic, сохранённый quarantine, отсутствие partial target и содержимое quarantine byte-for-byte.

### 7. Staging assertions не являются assertions

В Scenario 13 `STAGING_COUNT` вычисляется, но никак не проверяется; комментарий «may still exist (trap cleanup)» противоречит ожиданию уже завершившегося процесса — EXIT trap должен был очистить staging.

После завершения subprocess должно быть строго:

- zero staging directories этого запуска;
- target равен pre-transaction snapshot;
- zero quarantine при успешном restore;
- ровно одна сохранённая quarantine при искусственной ошибке restore.

Quarantine следует искать в точном cache root и связывать с текущим запуском, а не считать все старые диагностические каталоги, которые политика предписывает сохранять.

### 8. Missing-tool fixture неполон

Изолированный PATH создаётся через `ln`, но production preflight не перечисляет `dirname` и `rmdir`; сам script использует их. После добавления полного списка symlink fixture должен строиться из единого `REQUIRED_CMDS`, чтобы список теста и production не расходились.

Лучше разрешить test harness запросить/вывести required list, либо хранить его в одном sourced helper. Проверка только отсутствующего `curl` не доказывает полноту списка — нужен table-driven прогон хотя бы для каждого критичного инструмента или отдельный consistency test.

### 9. Verifier ошибочно описывает package-pass-before-test-pass

Строка 594 говорит «emit warning but continue; final assertion will catch it». Это неверно: если позже test перейдёт в `TEST_PASSED`, финальные состояния будут одновременно `TEST_PASSED` и `PKG_PASSED`, то есть событие будет принято.

Package terminal до test terminal необходимо отклонять **немедленно**.

Также:

- реальный `go test -json` выдаёт package action `start`; whitelist v6.4 его не содержит и отклонит нормальный поток;
- требуется явно определить поведение `pause`/`cont` в каждом состоянии;
- collision check должен выполняться до dispatch события в test state machine, иначе same-name event другого package может сначала изменить состояние;
- package identity нельзя выводить из произвольного первого same-name события без предварительной проверки collision; лучше передавать expected package runner-ом либо собрать множество и потребовать ровно один package;
- package `skip` и package `start` должны иметь формальный переход/ошибку.

Self-test должен использовать хотя бы один сохранённый реальный фрагмент `go test -json`, а не только вручную составленные события.

### 10. Persistence test остаётся псевдокодом и внутренне противоречив

`setupStore` содержит placeholder `... fixture creation ...`, после прямой записи не показана повторная загрузка store, а revision вычисляется из старого экземпляра. Таблица использует hardcoded `id1`, `valid-rev`, хотя helper возвращает реальные IDs/revision, которые игнорируются строкой:

```go
storePath, _, _ := setupStore(t)
```

В результате большинство кейсов не проверят заявленную ветку ошибки. Например, stale revision может сработать раньше selection mismatch, а nil/empty semantics зависят от фактического production contract.

Нужно дать компилируемый algorithm:

1. создать fixture;
2. загрузить его через `NewStore`;
3. проверить точные unsupported IDs/count;
4. получить fresh revision;
5. для каждого кейса построить IDs/revision **из реальных значений helper**;
6. вызвать операцию;
7. сравнить bytes;
8. reload и `reflect.DeepEqual`/`cmp.Diff` полного состояния, а не только ID.

Фраза «deep snapshot covers rules» противоречит коду, который сравнивает лишь длину и `ID`.

### 11. OpenAPI wrapper должен быть независим от случайного cwd

`go generate` сейчас запускает wrapper из `cmd/awg-manager`, и относительные аргументы swag с этим совпадают. Но сам wrapper вычисляет `REPO_ROOT`, не делает `cd`, поэтому прямой запуск `scripts/generate-openapi.sh` из корня или другого cwd даст иной результат/ошибку.

Нужно выполнить swag в явном subshell/cwd (`cd "$REPO_ROOT/cmd/awg-manager"`) либо использовать абсолютные пути. Добавить тест прямого запуска из `/tmp` и через `go generate`; результаты должны совпадать.

Patcher должен писать атомарно (temp в том же каталоге + replace), сохранять стабильный формат и завершаться ошибкой, если target schema/path не найден. Иначе «успешный» patch может ничего не изменить.

### 12. Счётчики сценариев и обязательность acceptance расходятся

План называет bootstrap suite «14 scenarios», но добавляет `13b`, то есть фактически сценариев минимум 15. Исправить нумерацию и expected final echo.

Full acceptance runner обозначен optional, хотя закрытие Stage 1 и walkthrough опираются на его фактический результат. Разделить:

- обязательные offline tests;
- обязательный acceptance run для финального утверждения о готовности;
- корректный `SKIP` без заявления о полном закрытии, если assets/env недоступны.

### 13. Acceptance checklist нельзя заранее отмечать выполненным

В плане критерии помечены `[x]`, хотя реализация ещё не выполнена и несколько критериев фактически не соблюдены. В implementation plan они должны быть `[ ]`. Отмечать их можно только в walkthrough после проверки артефактов и фактического вывода.

## Рекомендуемая редакция v6.5

1. Заменить NUL reader на `mapfile -d ''` с точным count и тестами 10/11/12/hanging-tail.
2. Исправить capture exit code `publish_cache`; сохранить различие exit 1/2.
3. Ввести два детерминированных failpoint: publish и restore.
4. Перестроить Scenario 13 вокруг pre-transaction snapshot и строгих assertions.
5. Вернуть exact-one version extraction, запрет partial `+build`, проверить actual banner variants.
6. Показать окончательную верхнюю часть bootstrap с реальным preflight до используемых инструментов.
7. Исправить verifier для `start`, package ordering, collision-before-dispatch и реального NDJSON fixture.
8. Переписать persistence test без placeholders/hardcoded IDs и сравнивать полную структуру.
9. Сделать OpenAPI wrapper cwd-independent и patch atomic/fail-closed.
10. Исправить число сценариев, обязательность acceptance и снять преждевременные `[x]`.

## Условия допуска к реализации

План можно отдавать агенту после того, как все P0 и P1 выше отражены в тексте и примеры становятся исполнимыми без скрытых предположений. Повторный аудит кода нужен уже после реализации; IPK и деплой на этом этапе не требуются.
