# Финальная перепроверка Remediation Plan v6.1

Дата: 2026-09-15  
План: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

v6.1 существенно доработан и включает все восемь замечаний предыдущего ревью. Архитектурно план правильный, но перед запуском требуется **короткая редакция v6.2**: три пункта сейчас не гарантируют достоверную и изолированную приёмку, ещё два желательно уточнить.

После обязательных поправок 1–3 план можно отдавать в реализацию без нового полного архитектурного ревью.

## Обязательные поправки

### 1. P1 — offline bootstrap tests должны использовать изолированный cache root

Предложенный `scripts/tests/test-bootstrap-acceptance.sh` запускает production bootstrap, который жёстко пишет в `$REPO_ROOT/.cache/mihomo`. Это загрязняет реальный cache рабочей копии, может взаимодействовать с параллельным acceptance run и делает тест потенциально разрушительным для уже загруженных fixtures.

Добавить в bootstrap явный параметр окружения:

```bash
CACHE_ROOT="${MIHOMO_ACCEPTANCE_CACHE_ROOT:-$REPO_ROOT/.cache/mihomo}"
```

Все lock, staging, target и quarantine пути должны строиться только от `CACHE_ROOT`. Integration-test обязан задавать `MIHOMO_ACCEPTANCE_CACHE_ROOT` равным каталогу внутри `mktemp -d`, проверить resolved path и очищать только собственный temp root.

Production runner не задаёт override и продолжает использовать repository cache. В production режиме override не должен ослаблять manifest/digest проверки.

### 2. P1 — checksum failure scenario сейчас логически противоречив

Пункт «checksum failure -> valid target remains untouched» нельзя вызвать простым изменением checksum в manifest: manifest SHA изменится, следовательно получится другой target path. Если target для того же manifest уже валиден, bootstrap завершится до скачивания и failure injection не сработает.

Нужно разделить тесты:

1. **Нет target:** сервер отдаёт повреждённый файл при неизменном manifest -> bootstrap возвращает non-zero, target не создан, staging очищен.
2. **Есть повреждённый target:** staging/download/checksum падает -> существующий повреждённый target остаётся на исходном пути и не переносится в quarantine до готовности нового staging.
3. **Есть валидный target:** сервер выключен -> bootstrap успешно завершает работу без HTTP-запросов и target byte-for-byte не меняется.

Из этого следует обязательный порядок production bootstrap: полностью скачать и проверить staging, и только затем под lock переместить старый повреждённый target в quarantine и немедленно опубликовать готовый staging. Нельзя quarantine старый target до успешной проверки нового поколения.

### 3. P1 — verifier должен иметь единственную реализацию

План предлагает оставить Python JSON verifier встроенным в runner и отдельно создать shell self-tests. Если тесты содержат копию алгоритма, они могут пройти после расхождения с реальной embedded-логикой runner.

Вынести verifier в один исполняемый файл, например:

```text
scripts/verify-mihomo-acceptance-json.py
```

Runner вызывает этот файл с путём JSON output и точным именем теста. Self-tests вызывают **тот же файл** на synthetic fixtures. Дублирование verifier logic в shell test запрещено.

Verifier должен fail-closed проверять:

- корректность хотя бы одного JSON event;
- target `run` ровно в допустимой последовательности до terminal event;
- ровно один terminal result для target;
- terminal result только `pass`;
- отсутствие `skip` и `fail`;
- package-level `fail` также приводит к ошибке;
- exit code `go test == 0` проверяется runner отдельно.

### 4. P2 — расширить корректную SemVer grammar

Regex в v6.1 не поддерживает полностью допустимые prerelease/build identifiers, содержащие дефис, и validation regex для manifest не содержит `+build`.

Либо использовать небольшую SemVer-функцию, либо разрешить:

```text
MAJOR.MINOR.PATCH[-0-9A-Za-z.-]+[+0-9A-Za-z.-]+
```

С обязательным полным совпадением строки после удаления только одного ведущего `v`. Добавить exact cases с `-rc.1`, `-rc-1` и `+build.7`, а также mismatch tests.

### 5. P2 — уточнить доказательство отсутствия повторной загрузки

Фраза «second run completes instantly without hitting HTTP server» должна проверяться счётчиком запросов, а не временем. Локальный server должен записывать число/пути запросов; после второго запуска счётчик не изменяется.

Также тест обязан корректно завершать server через trap и выбирать свободный loopback port без фиксированного номера.

## Остальная часть v6.1 одобрена

Следующие части можно выполнять как написано:

- exact-set equality, duplicate/empty rejection;
- handler/store defensive validation;
- restart persistence checks;
- DNS negative corpus;
- scoped locking и quarantine без автоматического удаления;
- cwd-independent runner;
- generated OpenAPI assertions;
- Go/race/frontend/idempotence/diff verification.

## Дополненный порядок приёмки

Перед общей матрицей необходимо подтвердить:

```text
offline test cache root находится только внутри собственного mktemp directory
production repository cache до и после offline tests имеет одинаковый список и digest файлов
failed download/checksum не создаёт target и не перемещает прежний target
quarantine выполняется только после полной проверки staging
runner и self-tests вызывают один и тот же verifier file
second bootstrap HTTP request counter delta == 0
все test HTTP server/background processes завершены
нет staging.* в test cache root после каждого сценария
```

## Решение

**Условно одобрено после v6.2.** Агенту достаточно внести пять уточнений выше в план; обязательными блокерами запуска являются пункты 1–3. IPK и деплой по-прежнему не входят в эту работу.
