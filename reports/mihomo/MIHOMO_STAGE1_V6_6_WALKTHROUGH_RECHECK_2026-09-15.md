# Повторный аудит walkthrough Mihomo Stage 1 v6.6

Дата: 2026-09-15  
Проверенный документ: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`  
Рабочая директория: `E:\AWGM\awg-manager`  
Ветка: `feature/mihomo-ai-proxyrt`

## Вердикт

Обновлённый walkthrough теперь в основном соответствует фактической реализации. Исправления предыдущего аудита действительно внесены: `rm` появился в preflight, коды сигналов стали корректными, Scenario 3 проверяет содержимое и retention quarantine, persistence output соответствует исходнику, URL-порты валидируются, а объём выполненных тестов описан честнее.

Однако финальное заявление walkthrough о полном закрытии всех замечаний пока принимать нельзя. Остаётся один существенный транзакционный дефект обработки сигналов во время публикации cache и два небольших расхождения между текстом отчёта и фактическим поведением.

Итоговая оценка: **условное принятие; перед окончательным закрытием Stage 1 исправить P1 ниже и скорректировать либо код, либо формулировки по P2**.

## Подтверждённые исправления

### 1. Preflight теперь учитывает `rm`

В `scripts/bootstrap-mihomo-acceptance.sh`:

- `rm` добавлен в `REQUIRED_CMDS`;
- preflight выполняется до первого внешнего вызова;
- Scenario 15 включает `rm` в матрицу реально отсутствующих инструментов.

Текущий перечень внешних команд production script вручную проверен — пропущенных команд не обнаружено.

### 2. Нулевой exit code при SIGINT/SIGTERM устранён

В production script обработчики разделены:

```bash
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
```

Scenario 16 реально проверяет оба кода (`143` и `130`), удаление staging, отсутствие нового target cache и неизменность старого cache.

### 3. Scenario 3 теперь доказывает quarantine retention

Тест использует отдельный `cache_s3`, сохраняет относительный snapshot повреждённого cache, сравнивает его с созданным quarantine и проверяет сохранение quarantine после следующего cache-hit запуска.

### 4. Persistence output исправлен

Walkthrough теперь перечисляет существующие в текущем исходнике под-тесты:

- `nil_IDs`;
- `empty_IDs`;
- `empty_string_in_IDs`;
- `duplicate_IDs`;
- `nonexistent_ID`;
- `stale_revision`;
- `subset_of_IDs`;
- `superset_of_IDs`.

### 5. URL validator усилен

Добавлены:

- принудительное чтение `p.port` с обработкой `ValueError`;
- тесты нечислового и выходящего за диапазон порта;
- проверки пробельных/управляющих символов внутри URL;
- дополнительные проверки IPv6 authority.

### 6. Scope тестов описан честнее

Walkthrough больше не называет четыре Go-пакета полным suite всего модуля и отдельно помечает `go test ./...` как `PENDING`. Race scope также назван `Selected Packages Race Detection`. Полный frontend lint честно записан как завершившийся с exit 1 из-за 30 существующих ошибок в других файлах.

## Оставшиеся замечания

### P1. Сигнал после quarantine старого cache может оставить target cache отсутствующим

`cleanup()` удаляет только временный fields-файл и staging. При публикации состояние меняется так:

1. существующий `CACHE_DIR` перемещается в `QUARANTINE_DIR`;
2. `PUBLICATION_STATE` становится `old_quarantined`;
3. staging перемещается в `CACHE_DIR`;
4. состояние становится `committed`.

Если SIGINT/SIGTERM будет обработан между шагами 2 и 3, signal handler немедленно завершит shell, а EXIT cleanup удалит staging, но не восстановит quarantine. В результате прежний cache останется только под диагностическим именем, а ожидаемый `CACHE_DIR` исчезнет.

Текущий Scenario 16 посылает сигналы только во время медленной загрузки, то есть до transaction boundary, поэтому эту ветку не покрывает.

Необходимое исправление:

1. Вынести state-aware recovery в функцию, вызываемую при выходе.
2. Если `PUBLICATION_STATE=old_quarantined`, `CACHE_DIR` отсутствует, а `QUARANTINE_DIR` существует — попытаться атомарно восстановить прежний cache.
3. При неудаче восстановления вернуть приоритетный recovery-required код (например, `2`) и оставить quarantine для ручного восстановления; не маскировать это кодом сигнала.
4. После `committed` откат не выполнять.
5. Добавить детерминированный failpoint/pause сразу после перехода в `old_quarantined` и тесты SIGTERM/SIGINT в этой точке.
6. Проверять byte-for-byte восстановление старого cache, отсутствие staging и отсутствие ложного success.

Это не опровергает нынешний Scenario 16, но означает, что signal safety проверена только для pre-commit загрузки, а не для всей транзакции публикации.

### P2. «Каждая используемая внешняя команда проверяется автоматически» — пока неверная формулировка

Scenario 15 содержит второй вручную записанный список:

```bash
for ext_cmd in rm python3 curl gzip ...; do
```

Он проверяет, что элементы этого списка присутствуют в `REQUIRED_CMDS`, но не обнаружит новую внешнюю команду, если разработчик добавит её в production script и забудет одновременно добавить в оба списка. Сейчас production list полон, поэтому функционального дефекта нет, но это не самоподдерживающаяся consistency-проверка.

Варианты закрытия:

- ослабить формулировку walkthrough до «явный проверяемый baseline внешних команд»;
- либо добавить независимый статический анализ production script (например, ShellCheck/AST-проверку) и сравнивать реально найденные команды с `REQUIRED_CMDS`.

### P2. Leading/trailing whitespace в URL не отклоняется, а молча обрезается

`validate_url()` сначала вызывает `validate_str()`, а тот выполняет `val.strip()`. Поэтому значение:

```text
 https://example.com/mihomo.gz 
```

принимается и возвращается уже без внешних пробелов. Независимая проверка текущего кода вернула:

```text
'https://example.com/mihomo.gz'
```

Это расходится с утверждением walkthrough о запрете whitespace «anywhere in the URL string».

Закрытие на выбор:

- если нормализация задумана — прямо задокументировать, что внешние пробелы обрезаются, а пробелы внутри URL запрещены;
- если требуется строгий trust boundary — проверять исходное значение до `.strip()` и добавить отдельный unit test для leading/trailing whitespace.

## Независимо выполненные проверки

В текущем workspace выполнено:

```text
bash -n scripts/bootstrap-mihomo-acceptance.sh scripts/tests/test-bootstrap-acceptance.sh
python3 -m unittest scripts/tests/test_validate_manifest.py
bash scripts/tests/test-bootstrap-acceptance.sh
go test -count=1 -run '^TestDeleteUnsupportedRules_PerErrorPersistence$' ./internal/mihomonative
git diff --check -- <проверенные Stage 1 файлы>
```

Фактический результат:

- shell syntax: PASS;
- manifest validator: 13/13 PASS;
- bootstrap acceptance: 16/16 PASS;
- persistence parity test: PASS;
- whitespace check: PASS.

IPK не собирался. Деплой не выполнялся. Полный `go test ./...`, полный frontend suite, production build и race suite в этом повторном аудите заново не запускались; для них проверены предоставленные в walkthrough результаты и честность указанного scope.

## Что передать следующему агенту

Минимальная следующая итерация:

1. Исправить state-aware recovery при SIGINT/SIGTERM после `old_quarantined`.
2. Добавить signal-at-publication acceptance scenario.
3. Выбрать строгую или нормализующую политику внешних пробелов URL и синхронизировать код, тест и walkthrough.
4. Исправить формулировку consistency check либо сделать её действительно независимой.
5. Повторить 16 bootstrap scenarios (или увеличить их число), validator unittest и mandatory acceptance runner.
6. После этого обновить финальную матрицу и только тогда заявлять полное закрытие Stage 1.

