# Финальный повторный аудит Mihomo Stage 1 v6.6

Дата: 2026-09-15  
Проверенный walkthrough: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`  
Workspace: `E:\AWGM\awg-manager`  
Ветка: `feature/mihomo-ai-proxyrt`

## Вердикт

Все конкретные замечания из `MIHOMO_STAGE1_V6_6_FINAL_CLOSURE_RECHECK_2026-09-15.md` теперь исправлены:

- recovery вооружён до опасного rename;
- cleanup понимает pending/quarantined состояния;
- Scenario 17 использует явные ready markers;
- добавлен post-`mv` сигнал в pending state;
- URL whitespace проверяется строго;
- command analyzer получил mutation tests и двустороннюю parity-проверку;
- обязательная проверка настоящим Mihomo binary проходит.

Однако «unconditional final closure» всё ещё требует одной маленькой правки в `cleanup()`: некритичные операции удаления сейчас выполняются раньше восстановления cache и не защищены от ошибки при активном `set -e`. Это новый найденный отказоустойчивый edge case, а не невыполнение предыдущей remediation.

Итог: **функциональная часть Stage 1 принята; для строгой транзакционной гарантии исправить P1 ниже**.

## Подтверждённые исправления

### 1. Rename window защищён

Перед `mv "$CACHE_DIR" "$QUARANTINE_DIR"` устанавливается:

```bash
PUBLICATION_STATE="old_quarantine_pending"
```

Если сигнал обработан после фактического rename, cleanup видит отсутствующий `CACHE_DIR` и существующий quarantine, после чего восстанавливает старый cache. Если rename ещё не состоялся, `CACHE_DIR` существует и восстановление не требуется.

### 2. Scenario 17 синхронизирован

Тест больше не полагается только на появление имени `quarantine.*`. Для обычных pending/quarantined окон используются явные ready marker-файлы. Дополнительный hook `_BOOTSTRAP_TEST_SIGNAL_QUARANTINE_PENDING_POST_MV=1` проверяет отложенный сигнал сразу после rename и до смены состояния на `old_quarantined`.

Тесты проверяют:

- коды `130`/`143`;
- отсутствие staging;
- отсутствие orphan quarantine после успешного restore;
- byte-for-byte равенство восстановленного cache;
- отсутствие отката после commit boundary.

### 3. Command analyzer усилен

Подтверждены:

- inline environment assignments;
- leading redirections;
- nested command substitutions;
- process substitutions;
- pipeline/logical separators;
- обнаружение undeclared и unused declarations;
- production parity.

`find` удалён из `REQUIRED_CMDS`, так как production bootstrap его не вызывает.

### 4. Walkthrough честно ограничивает scope

Полный `go test ./...` остаётся `PENDING`; affected package scope и frontend lint failures описаны отдельно. Это корректно и не выдаётся за полностью зелёный репозиторий.

## Оставшееся замечание

### P1. Ошибка best-effort cleanup может не дать восстановить cache

Текущий `cleanup()` начинает работу так:

```bash
if [ -n "$FIELDS_TMP" ] && [ -f "$FIELDS_TMP" ]; then
    rm -f "$FIELDS_TMP"
fi
if [ -n "$CLEANUP_STAGING_DIR" ] && [ -d "$CLEANUP_STAGING_DIR" ]; then
    rm -rf "$CLEANUP_STAGING_DIR"
fi

# Только затем state-aware recovery
```

Production script работает с `set -euo pipefail`. Команды `rm` не находятся в защищённом `if ! ...` и не переводятся в best-effort режим. Если удаление fields или staging завершится ошибкой (I/O error, read-only mount, permission/corruption), EXIT trap может прекратить выполнение до блока state-aware recovery.

В состоянии `old_quarantine_pending`/`old_quarantined` это означает:

1. старый cache уже может находиться в quarantine;
2. ожидаемый `CACHE_DIR` отсутствует;
3. ошибка удаления staging обрывает trap;
4. quarantine не восстанавливается;
5. исходный signal/error code также может быть заменён кодом ошибки `rm`.

Это нарушает приоритет восстановления пользовательского cache над уборкой временных файлов.

## Требуемое исправление

### Изменить порядок cleanup

1. В самом начале сохранить исходный код выхода.
2. Отключить неявное прекращение cleanup на вспомогательной ошибке (`set +e`) либо полностью оборачивать каждую потенциально падающую команду.
3. Сначала выполнить state-aware recovery старого cache.
4. Только после успешного восстановления выполнять best-effort удаление fields/staging.
5. Если restore невозможен — вернуть `2` независимо от исходного signal code и оставить quarantine.
6. Если restore не требовался/успешен, а временная уборка не удалась — вывести предупреждение; политика exit code должна быть задана явно, но не должна маскировать уже существующий ненулевой код.
7. Перед окончательным `exit` отключить EXIT trap (`trap - EXIT`), чтобы семантика не зависела от повторного `exit` внутри handler.

Пример структуры:

```bash
cleanup() {
    local original_rc=$?
    local cleanup_rc=0
    trap - EXIT
    set +e

    recover_cache_if_required || exit 2

    remove_fields_best_effort || cleanup_rc=1
    remove_staging_best_effort || cleanup_rc=1

    if [ "$original_rc" -ne 0 ]; then
        exit "$original_rc"
    fi
    exit "$cleanup_rc"
}
```

Точная политика может отличаться, но recovery старого cache должен выполняться до некритичной уборки.

### Добавить fault-injection test

Добавить сценарий, который одновременно:

- переводит bootstrap в `old_quarantined`;
- имитирует ошибку удаления staging/fields;
- завершает процесс сигналом либо обычной ошибкой;
- доказывает, что старый cache всё равно восстановлен byte-for-byte;
- доказывает приоритет exit `2`, если само восстановление не удалось;
- проверяет, что диагностический quarantine не уничтожен при recovery failure.

Для детерминизма предпочтительнее отдельный test hook ошибки cleanup, а не изменение разрешений каталога, которое нестабильно при запуске от root.

## Неблокирующее замечание к command analyzer

Документация теперь правильно называет его эвристическим. Это важно: он всё ещё не является полноценным shell parser. Например, wrappers наподобие `env FOO=1 tool` потребуют отдельной логики, а имя команды с точкой отбрасывается условием `if "." in first`.

Для текущего production bootstrap ручная проверка и unit tests подтверждают полный используемый набор, поэтому это не блокирует Stage 1. В будущем при усложнении shell script лучше перейти на ShellCheck AST либо добавить mutation cases для wrappers и executable names с точкой.

Отдельная терминологическая мелочь: `pwd` является builtin Bash, хотя анализатор считает его внешней командой и включает в `REQUIRED_CMDS`. Это не ломает выполнение, но формулировка «17 external commands» технически неточна.

## Независимые проверки

Выполнены:

```text
bash -n scripts/bootstrap-mihomo-acceptance.sh scripts/tests/test-bootstrap-acceptance.sh
python3 -m unittest scripts/tests/test_verify_bootstrap_commands.py scripts/tests/test_validate_manifest.py
python3 scripts/tests/verify_bootstrap_commands.py
bash scripts/tests/test-bootstrap-acceptance.sh
bash scripts/run-mihomo-acceptance.sh
git diff --check -- <изменённые Stage 1 scripts/tests>
```

Результат:

- Python tests: 24/24 PASS;
- command parity: 17 invoked / 17 declared, PASS;
- bootstrap acceptance: 17/17 PASS;
- real Mihomo binary acceptance: PASS;
- shell syntax: PASS;
- targeted whitespace check: PASS.

IPK не собирался. Деплой не выполнялся. Полный module/frontend/race scope в этом recheck заново не выполнялся.

## Минимальная следующая итерация

1. Переставить state-aware recovery перед удалением временных файлов.
2. Сделать все cleanup-команды явными и не зависящими от `set -e`.
3. Добавить fault injection ошибки временной уборки при quarantined state.
4. Повторить 17 bootstrap scenarios, Python unit tests и mandatory real-binary runner.
5. После этого транзакционную часть Stage 1 можно закрыть окончательно.

