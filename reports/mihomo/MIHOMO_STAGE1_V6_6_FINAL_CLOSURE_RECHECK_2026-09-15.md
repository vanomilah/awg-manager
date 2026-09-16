# Повторная проверка финального закрытия Mihomo Stage 1 v6.6

Дата: 2026-09-15  
Проверенный walkthrough: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`  
Workspace: `E:\AWGM\awg-manager`  
Ветка: `feature/mihomo-ai-proxyrt`

## Вердикт

Предыдущая remediation действительно реализована, а обновлённый bootstrap harness проходит все 17 сценариев. Строгая проверка внешних пробелов URL также работает.

Тем не менее утверждение walkthrough о **Final Closure** пока не подтверждено. В state-aware recovery остаётся узкое, но реальное окно между успешным перемещением старого cache и присвоением `PUBLICATION_STATE="old_quarantined"`. Новый Scenario 17 это окно не тестирует и сам имеет недостаточно строгую синхронизацию.

Итог: **условное принятие; нужен ещё один небольшой P1 patch и повторный Scenario 17**.

## Что подтверждено

### State-aware cleanup добавлен

`cleanup()` теперь:

- сохраняет исходный exit code;
- удаляет fields/staging;
- при состоянии `old_quarantined` и отсутствующем `CACHE_DIR` восстанавливает `QUARANTINE_DIR`;
- при невозможности восстановления возвращает recovery-required code `2`;
- после успешного восстановления сохраняет signal code `130`/`143`.

При сигнале во время специально добавленной паузы после quarantine восстановление действительно происходит byte-for-byte.

### URL whitespace исправлен

`validate_url()` проверяет исходное строковое значение до нормализации и отклоняет leading/trailing whitespace. Добавленные случаи присутствуют в unittest и проходят.

### Автоматическая проверка внешних команд подключена

`scripts/tests/verify_bootstrap_commands.py` вызывается из Scenario 15 и проверяет, что найденные анализатором внешние команды объявлены в `REQUIRED_CMDS`.

### Scope walkthrough описан честно

- четыре Go-пакета названы affected packages, а не полным module suite;
- race detector scope указан отдельно;
- `go test ./...` оставлен `PENDING`;
- полный frontend lint отмечен как `NOTE / exit 1`, а не PASS.

## Оставшиеся замечания

### P1. Recovery state устанавливается после опасного `mv`

Текущая последовательность в `publish_cache()`:

```bash
if ! mv "$CACHE_DIR" "$QUARANTINE_DIR"; then
    ...
fi
PUBLICATION_STATE="old_quarantined"
```

Bash откладывает выполнение trap, пока foreground-команда завершается. Если SIGINT/SIGTERM приходит во время `mv`, возможна последовательность:

1. `mv` успешно перемещает старый cache в quarantine;
2. отложенный signal trap выполняется до следующего shell statement;
3. `PUBLICATION_STATE` всё ещё равно `prepared`;
4. EXIT cleanup не запускает state-aware recovery;
5. staging удаляется, ожидаемый `CACHE_DIR` отсутствует, старый cache остаётся под именем quarantine.

Необходимое исправление — вооружить recovery **до** опасного rename:

```bash
PUBLICATION_STATE="old_quarantine_pending"
if ! mv "$CACHE_DIR" "$QUARANTINE_DIR"; then
    PUBLICATION_STATE="prepared"
    ...
fi
PUBLICATION_STATE="old_quarantined"
```

Cleanup должен обрабатывать оба состояния (`old_quarantine_pending` и `old_quarantined`) по фактическому состоянию файловой системы:

- если `CACHE_DIR` существует — старый cache не потерян, restore не нужен;
- если `CACHE_DIR` отсутствует и quarantine существует — восстановить quarantine;
- если оба отсутствуют либо restore не удался — вывести `RECOVERY-REQUIRED` и вернуть `2`;
- после `committed` откат не выполнять.

Альтернатива — присвоить `old_quarantined` непосредственно перед `mv` и при обычной ошибке `mv` вернуть состояние в `prepared`; проверка существования `CACHE_DIR` в cleanup уже предотвращает ложный restore.

### P1. Scenario 17 не синхронизирован с фактическим переходом состояния

Сейчас тест посылает сигнал, как только `find` увидел имя `quarantine.*`:

```bash
if [ "${#S17_Q_DIRS[@]}" -gt 0 ]; then
    break
fi
```

Но такое имя появляется уже при `mktemp -d`, до:

- `rmdir` резервного каталога;
- `mv CACHE_DIR QUARANTINE_DIR`;
- присвоения `PUBLICATION_STATE="old_quarantined"`;
- входа в `_BOOTSTRAP_PAUSE_OLD_QUARANTINED`.

Следовательно, тест может послать сигнал до проверяемой точки. В частности, сигнал после `rmdir`, но до `mv` оставит исходный cache неизменным и ноль quarantine-каталогов — текущие assertions могут ошибочно принять это за успешный rollback.

Нужно сделать явную синхронизацию:

1. Production test hook после присвоения recovery-armed state создаёт ready marker либо пишет однозначную строку в отдельный log.
2. Harness ждёт именно marker/строку `PAUSE_OLD_QUARANTINED`, а не просто имя каталога.
3. Только после этого отправляет SIGTERM/SIGINT.
4. Отдельный failpoint должен покрывать сигнал во время rename window после предварительного вооружения recovery.

### P2. Статический анализатор команд является эвристическим, а не shell parser

Текущий `verify_bootstrap_commands.py` полезен для существующего script, но не гарантирует обнаружение любой будущей команды. Например:

```python
extract_commands("FOO=1 undeclared_tool arg") == set()
```

То есть команда после inline environment assignment пропускается. Регулярное выражение для `$()` также не является полноценным разбором вложенного shell syntax.

Кроме того, проверяется только `invoked - declared`, поэтому лишние обязательные зависимости не выявляются. Сейчас `find` объявлен в `REQUIRED_CMDS`, хотя production bootstrap его не вызывает.

Это не блокирует текущий bootstrap: ручная проверка существующего script не нашла пропущенных external commands. Но walkthrough должен называть это **эвристической статической проверкой**, а не исчерпывающим доказательством.

Рекомендуется:

- добавить mutation unit tests анализатора, включая inline env assignment, команды после redirections и вложенные substitutions;
- проверять отдельно `undeclared` и `unused_declared` либо документировать допустимые исключения;
- по возможности заменить самописный разбор на ShellCheck AST/JSON или другой настоящий shell parser.

## Независимые проверки

Выполнено в текущем workspace:

```text
bash -n scripts/bootstrap-mihomo-acceptance.sh scripts/tests/test-bootstrap-acceptance.sh
python3 -m unittest scripts/tests/test_validate_manifest.py
python3 scripts/tests/verify_bootstrap_commands.py
bash scripts/tests/test-bootstrap-acceptance.sh
```

Результаты:

- shell syntax: PASS;
- manifest validator: 13/13 PASS;
- command analyzer на текущем production script: PASS, найдено 17 invoked commands;
- bootstrap harness: 17/17 PASS;
- Scenario 17 в текущей реализации проходит для SIGTERM и SIGINT.

Эти результаты подтверждают реализованные happy/error paths, но не закрывают описанное выше окно сигналов во время `mv`.

IPK не собирался. Деплой не выполнялся. Полный Go/module suite, frontend suite, build и race suite в этом повторном recheck заново не запускались.

## Минимальная следующая итерация

1. Вооружить recovery state до `mv CACHE_DIR -> QUARANTINE_DIR`.
2. Сделать Scenario 17 синхронизированным с явным ready marker после перехода состояния.
3. Добавить отдельную проверку signal/exit непосредственно в rename window.
4. Уточнить формулировку command analyzer и добавить хотя бы mutation tests для известных слепых зон.
5. Повторить validator, command analyzer, bootstrap harness и mandatory real-binary acceptance runner.
6. После этого Final Closure можно принимать при условии отсутствия новых расхождений.

