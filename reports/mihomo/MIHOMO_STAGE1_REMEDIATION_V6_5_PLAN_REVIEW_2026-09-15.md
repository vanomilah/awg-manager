# Review: Mihomo Stage 1 Remediation v6.5

Дата: 2026-09-15  
Проверенный план: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

**v6.5 почти готов к реализации, но буквальный запуск пока не одобрен.** Большинство замечаний v6.4 устранено: исправлены NUL reader, exact-one version matching, два failpoint, package lifecycle, cwd-independent OpenAPI wrapper, реальные IDs/revision в persistence tests и корректный checklist.

Осталась небольшая, но обязательная дельта v6.6. После исправления P0/P1 ниже план можно отдавать агенту без нового полного переписывания.

## Закрытые замечания v6.4

- Используется `mapfile -d ''` и проверка количества 11 полей.
- Код возврата `publish_cache` больше не читается после `!`.
- Добавлены отдельные failpoints публикации и восстановления.
- Version matcher извлекает все banner-кандидаты и требует ровно один.
- Полный version token проверяется отдельно, поэтому `+build` не обрезается.
- Verifier знает package action `start` и предварительно проверяет collision.
- OpenAPI wrapper выполняет swag в явном каталоге.
- Persistence cases строятся из реальных IDs и revision.
- Bootstrap suite теперь правильно называется набором из 15 сценариев.
- Acceptance criteria больше не отмечены выполненными заранее.

## P0 — исправить до реализации

### 1. `set +e` отключает fail-fast внутри всей функции публикации

План вызывает:

```bash
set +e
publish_cache
rc=$?
set -e
```

Это сохраняет status, но одновременно отключает `errexit` для всех команд внутри `publish_cache`. Команды `mkdir`, `mktemp`, `rmdir` и возможные будущие команды могут завершиться ошибкой, после чего функция продолжит работу с некорректным состоянием.

Не полагаться на `set -e` внутри транзакции вообще. Каждая изменяющая состояние операция должна иметь явный `if ! command; then ...; return ...; fi`. После этого функцию можно вызывать через контролируемый capture status. В частности явно проверить:

- `mkdir -p parent`;
- `mktemp -d` для quarantine;
- `rmdir` зарезервированного имени;
- оба `mv`;
- создание staging и marker вне функции.

Рекомендуемый вызов:

```bash
rc=0
publish_cache || rc=$?
if [ "$rc" -ne 0 ]; then
    ...
fi
```

Но важно: функция в правой части `||` также не должна зависеть от неявного `errexit`; все критичные команды внутри должны проверяться явно.

### 2. Scenario 13 всё ещё снимает snapshot до изменения pre-transaction state

Последовательность v6.5:

1. добавить marker;
2. снять snapshot;
3. удалить `.bootstrap_complete`;
4. начать транзакцию.

Следовательно, cache непосредственно перед транзакцией уже отличается от snapshot: marker `.bootstrap_complete` отсутствует. Корректный rollback восстановит состояние без marker, и сравнение со снимком упадёт.

Нужно сначала удалить `.bootstrap_complete`, затем снять `s13_pre` и modes snapshot, после чего запускать bootstrap. То же правило применить к Scenario 14.

### 3. Сравнение quarantine с исходным cache должно использовать относительные пути

Строки `find ... -exec sha256sum {} + | sort` включают полный путь в output. После перемещения cache в quarantine содержимое то же, но имена корневых каталогов разные, поэтому текстовые snapshots различаются.

Создать helper `snapshot_tree ROOT`, который выводит относительно `ROOT`:

- тип записи;
- относительный path;
- mode;
- SHA-256 для обычных файлов;
- symlink target, если symlinks допустимы.

Сравнивать output helper для `CACHE_DIR` и `QUARANTINE_DIR`. Это одновременно реализует заявленную, но пока не показанную проверку permissions/modes.

### 4. Scenario 15 не найдёт `bash` в изолированном PATH

План запускает:

```bash
PATH="$ISOLATED_DIR" bash "$BOOTSTRAP_SCRIPT" ...
```

Но `bash` отсутствует в `REQUIRED_CMDS` и, следовательно, symlink для него не создаётся. Тест завершится ошибкой `bash: command not found` до проверки preflight и даст ложный результат.

Сохранить абсолютный путь заранее:

```bash
BASH_BIN="$(command -v bash)"
PATH="$ISOLATED_DIR" "$BASH_BIN" "$BOOTSTRAP_SCRIPT" ...
```

Также тест использует `tr`, `ln`, `grep` и другие harness tools. Они могут выполняться вне изолированного subprocess, но это следует показать явно. Запрещено добавлять `bash` в production `REQUIRED_CMDS`: интерпретатор уже выбран shebang/явным запуском и не является внутренней зависимостью bootstrap.

## P1 — обязательные уточнения

### 5. Старые quarantine ломают assertions `zero` и `exactly one`

Политика предписывает сохранять quarantine после успешной замены повреждённого cache. Scenario 3 выполняется раньше Scenario 13/14 и может оставить диагностический каталог в общем cache root. Поэтому глобальные проверки «quarantine count == 0» и «count == 1» нестабильны.

Каждый сценарий должен использовать собственный `MIHOMO_ACCEPTANCE_CACHE_ROOT`, либо тест должен сохранить baseline set quarantine и проверять только дельту/конкретный путь текущего запуска. Изоляция root на сценарий предпочтительнее.

### 6. Trap после успешной публикации снимается слишком широко

`trap - EXIT` удаляет весь EXIT handler. Сейчас это cleanup staging, но дальнейшие изменения легко добавят другие cleanup actions. Лучше иметь единый cleanup dispatcher и после commit присвоить `STAGING_DIR=""`, не снимать trap. Тогда handler безопасно ничего не удалит для staging, но сохранит прочие cleanup obligations.

Отдельно проверить: при exit 2 staging удаляется, quarantine сохраняется.

### 7. Manifest NUL contract требует проверки незавершённого последнего поля

`mapfile -d ''` с count 11 может принять поток, где одиннадцатое поле завершилось EOF, а не NUL. Поскольку standalone validator является частью trust boundary, bootstrap должен либо проверить, что последний байт файла NUL, либо validator tests и invocation contract должны исключать замену/повреждение helper.

Добавить bootstrap self-test для 11 полей без финального NUL и ожидать отказ. Реализовать проверку последнего байта без хранения NUL в shell variable, например Python validator-output checker или `od`/`tail` с включением этих инструментов в preflight.

### 8. Verifier должен явно определить `start` только для package events

Whitelist общий, но test state machine не принимает `start`. Зафиксировать:

- `start` допустим только для event без `Test` и с target package;
- package `start` допускается ровно один и только до package output/terminal согласно выбранному контракту;
- package terminal до target test terminal отклоняется немедленно;
- события других тестов/пакетов игнорируются только после JSON/schema validation и не меняют target machines.

Реальный NDJSON fixture должен быть закоммичен как отдельный testdata-файл или генерироваться реальным `go test -json` в тесте. Фраза «captured fixture» без пути к артефакту недостаточна.

### 9. Persistence fixture использует private store internals — это допустимо только в same-package test

Код обращается к `s.mu`, `s.data`, `saveLocked` и типу `state`. Файл должен оставаться с `package mihomonative`, а не `mihomonative_test`. Это нужно записать явно.

Также `expectedState := store.data` — shallow copy со slices/pointers. Для строгой проверки snapshot лучше deep-clone до операции через JSON round-trip или сравнить `state` из `beforeBytes` с reopened state. Текущий byte equality уже доказывает disk immutability, но заявленная memory/deep гарантия должна использовать независимый pre-operation snapshot, а не state, прочитанный после rejected operation.

### 10. OpenAPI patcher должен гарантировать cleanup temp и стабильное YAML-представление

Уточнить:

- temp создаётся через `tempfile.NamedTemporaryFile(delete=False, dir=target.parent)` или `mkstemp`, а не только `path + pid`;
- temp удаляется при любой ошибке;
- flush + `fsync` файла выполняются перед `os.replace` (если заявляется crash-safe atomic publication);
- YAML dump сохраняет стабильный порядок/формат, чтобы первый запуск не переформатировал весь 500 KB spec;
- patcher проверяет тип каждого узла и после записи повторно читает/проверяет `minLength == 1`.

Если требуется только atomic visibility, а не crash durability, не заявлять более сильную гарантию.

### 11. Финальная проверка должна включать lint и actual clean-scope diff

В текущем CI frontend есть `npm run lint`, а в verification matrix v6.5 lint отсутствует. Добавить его.

Репозиторий уже сильно dirty, поэтому `git diff --check` недостаточен для доказательства отсутствия посторонних изменений. Перед реализацией сохранить baseline `git status --short`; после работы перечислить точные затронутые файлы и проверить diff только scoped files. Не удалять и не перезаписывать пользовательские untracked файлы.

## Минимальная редакция v6.6

1. Явно проверять каждую state-changing command внутри publication function.
2. Перенести snapshot после удаления `.bootstrap_complete`.
3. Ввести `snapshot_tree` с относительными путями и modes.
4. Запускать bootstrap в isolated PATH через абсолютный `$BASH_BIN`.
5. Изолировать cache root каждого scenario.
6. Не снимать общий EXIT trap; очищать через состояние переменных.
7. Проверять финальный NUL validator output.
8. Уточнить package/test event dispatch и расположение real NDJSON fixture.
9. Делать независимый pre-operation state snapshot в persistence test.
10. Уточнить temp cleanup/stable serialization patcher и добавить frontend lint.

## Итог для агента

Это уже не требует нового архитектурного проектирования. Выпустить v6.6 как точечную правку перечисленных мест. После неё план можно запускать в реализацию; следующая проверка должна быть короткой проверкой дельты, а не новым полным аудитом.
