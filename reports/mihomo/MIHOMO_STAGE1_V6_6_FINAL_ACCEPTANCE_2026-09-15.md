# Финальная приёмка Mihomo Stage 1 v6.6

Дата: 2026-09-15  
Проверенный walkthrough: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`  
Workspace: `E:\AWGM\awg-manager`  
Ветка: `feature/mihomo-ai-proxyrt`

## Вердикт

**Production-реализация транзакционного bootstrap и обязательного acceptance-контура принята.** Последний блокирующий P1 из `MIHOMO_STAGE1_V6_6_FINAL_AUDIT_RECHECK_2026-09-15.md` исправлен корректно.

State-aware recovery теперь выполняется до best-effort уборки, EXIT handler не зависит от `set -e`, исходный ненулевой код сохраняется, а невозможность восстановления получает приоритетный код `2`. Независимый прогон всех относящихся к изменению тестов прошёл.

Остаётся одно небольшое P2-расхождение в доказательной строгости walkthrough: double-failure subtest проверяет наличие сохранённого quarantine, но не его byte-for-byte содержимое. Это не обнаруженный дефект production-кода и не блокирует Stage 1, однако перед формальным утверждением «quarantine preserved intact» тест стоит усилить.

## Подтверждённые свойства production-кода

### 1. Восстановление cache имеет высший приоритет

`cleanup()` выполняет действия в правильном порядке:

1. сохраняет исходный exit code;
2. отключает повторный EXIT trap через `trap - EXIT`;
3. выполняет `set +e`, исключая неявный обрыв handler;
4. восстанавливает cache из quarantine, если состояние `old_quarantine_pending` или `old_quarantined` и основной cache отсутствует;
5. только затем выполняет best-effort удаление fields/staging;
6. возвращает исходный ненулевой код либо cleanup code для изначально успешного выхода.

Это закрывает прежний сценарий, при котором ошибка `rm` могла оборвать EXIT trap до восстановления пользовательского cache.

### 2. Приоритет exit code определён корректно

- успешное восстановление после SIGTERM сохраняет `143`;
- успешное восстановление после SIGINT сохраняет `130`;
- обычная исходная ошибка не маскируется ошибкой временной уборки;
- невозможность восстановить cache немедленно возвращает `2` и сохраняет quarantine для ручного восстановления;
- cleanup error используется только если первоначальный exit был успешным.

### 3. Ошибки временной уборки больше не блокируют recovery

Операции удаления обёрнуты в явные проверки. Ошибки преобразуются в предупреждение и `cleanup_rc=1`, а не прерывают handler. Fault-injection staging failure подтверждает, что cache сначала восстанавливается byte-for-byte, после чего сохраняется signal code `143`.

### 4. Rename-window защита сохранена

Recovery вооружён состоянием `old_quarantine_pending` до `mv CACHE_DIR -> QUARANTINE_DIR`. Scenario 17 покрывает:

- сигнал до rename;
- сигнал сразу после rename, но до `old_quarantined`;
- SIGTERM/SIGINT в `old_quarantined`;
- ошибку временной уборки после восстановления;
- одновременную невозможность восстановления.

### 5. Acceptance остаётся привязан к настоящему Mihomo

`scripts/run-mihomo-acceptance.sh` использовал cached real Mihomo binary и подтвердил целевой lifecycle test `TestGenerateMihomoConfig_RepresentativeBinaryValidation` для пакета `internal/singbox/router`.

## Единственное оставшееся замечание

### P2. Double-failure subtest не доказывает `quarantine preserved intact`

Walkthrough утверждает, что subtest 7 подтверждает строгое сохранение diagnostic quarantine. Фактически тест после recovery failure проверяет:

```bash
mapfile -d '' S17_DOUBLE_Q < <(find ... -name "quarantine.*" -print0)
if [ "${#S17_DOUBLE_Q[@]}" -ne 1 ]; then
    ...
fi
```

То есть доказано:

- exit code равен `2`;
- выведено сообщение `RECOVERY-REQUIRED`;
- существует ровно один quarantine-каталог.

Но перед запуском не создаётся snapshot повреждённого `S17_CACHE_DIR`, а содержимое найденного quarantine не сравнивается с исходным cache. Поэтому слово `intact` пока является предположением по поведению `mv`, а не проверенным assertion.

Рекомендуемая небольшая доработка:

```bash
S17_PRE_DOUBLE="$TMP_DIR/s17_pre_double.tree"
snapshot_tree "$S17_CACHE_DIR" > "$S17_PRE_DOUBLE"

# после recovery failure
S17_DOUBLE_Q_TREE="$TMP_DIR/s17_double_q.tree"
snapshot_tree "${S17_DOUBLE_Q[0]}" > "$S17_DOUBLE_Q_TREE"
cmp -s "$S17_PRE_DOUBLE" "$S17_DOUBLE_Q_TREE" || fail
```

Также стоит проверить, что `CACHE_DIR` отсутствует в recovery-required состоянии, чтобы состояние файловой системы было доказано полностью.

Это P2-тестовый пробел; production recovery logic по прочитанному коду корректна.

## Неблокирующие замечания к walkthrough

1. Фраза «Disables `set +e`» ошибочна: код выполняет `set +e`, то есть отключает поведение `set -e`. Формулировку следует заменить на «disables errexit with `set +e`».
2. Static command analyzer правильно назван эвристическим, но `pwd` является builtin Bash, поэтому фраза «17 external commands» технически условна. Preflight при этом безопасен и работоспособен.
3. Полный `go test ./...` честно оставлен `PENDING`; следовательно, текущая приёмка относится к Stage 1 и affected scopes, а не ко всему огромному dirty workspace.

## Независимо выполненные проверки

Выполнено:

```text
bash -n scripts/bootstrap-mihomo-acceptance.sh scripts/tests/test-bootstrap-acceptance.sh
python3 -m unittest scripts/tests/test_verify_bootstrap_commands.py scripts/tests/test_validate_manifest.py
python3 scripts/tests/verify_bootstrap_commands.py
bash scripts/tests/test-bootstrap-acceptance.sh
bash scripts/run-mihomo-acceptance.sh
git diff --check -- <изменённые Stage 1 scripts/tests>
```

Результаты:

- Python unit tests: **24/24 PASS**;
- command parity: **17 declared / 17 detected, PASS**;
- bootstrap acceptance: **17/17 PASS**;
- real Mihomo binary acceptance: **PASS**;
- shell syntax: **PASS**;
- targeted whitespace/conflict check: **PASS**.

IPK не собирался. Деплой не выполнялся. Полный Go module suite, весь frontend suite и race suite в этой финальной проверке заново не запускались.

## Решение для следующего агента

1. Stage 1 production bootstrap можно считать принятым.
2. Добавить byte-for-byte assertion для quarantine в Scenario 17 subtest 7.
3. Исправить две неточные формулировки walkthrough.
4. После этого документацию можно пометить как полностью закрытую без дополнительных изменений production-кода.

