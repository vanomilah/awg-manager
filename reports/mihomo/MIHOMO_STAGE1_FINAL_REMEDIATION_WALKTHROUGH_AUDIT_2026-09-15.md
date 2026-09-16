# Аудит walkthrough: Mihomo Core Stabilization Stage 1

Дата проверки: 2026-09-15  
Проверенный документ: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`  
Рабочая копия: `E:\AWGM\awg-manager`, ветка `feature/mihomo-ai-proxyrt`

## Вердикт

Заявление walkthrough о полном закрытии Stage 1 **не подтверждено**. Основные пакеты компилируются и их существующие тесты проходят, однако в новом destructive API удаления legacy-правил остался дефект целостности выбора, контракт пустого запроса реализован противоположно согласованному, DNS-валидация остаётся fail-open для нескольких синтаксически неверных значений, а acceptance bootstrap/runner не готов к безопасному повторному и параллельному запуску.

До устранения P0/P1 и повторной приёмки Stage 1 нельзя считать закрытым и нельзя использовать кнопку массового удаления как безопасную миграционную операцию.

## Найденные проблемы

### P0 — дубли ID позволяют удалить правила, которых пользователь не подтвердил

`Store.DeleteUnsupportedRules` сравнивает только длину входного списка с длиной текущего снимка, а затем проверяет принадлежность каждого входного ID множеству текущих ID (`internal/mihomonative/store.go:915-926`). Уникальность `expectedIDs` не проверяется.

Пример: текущий снимок содержит `A, B`, запрос содержит `A, A`. Длины совпадают, обе проверки membership проходят. Затем удаление выполняется по `currentMap`, содержащему `A` и `B` (`store.go:932-945`), поэтому удалятся **оба** правила, хотя клиент подтвердил только `A`.

Это нарушает заявленный full-set contract и границу destructive confirmation.

Исправление:

1. До сравнения множеств отклонять пустые ID и дубли.
2. Построить `expectedSet` и требовать точного равенства множеств `expectedSet == currentSet`.
3. Удалять только после успешного точного сравнения.
4. Добавить store- и HTTP-тест `current=[A,B], request=[A,A]` с ожиданием `400 SELECTION_MISMATCH`; убедиться, что на диске и в памяти остаются оба правила.

### P1 — пустой список принят как успешная destructive-операция

HTTP handler проверяет только непустую `revision`, но не `ids` (`internal/api/mihomo_handler.go:432-444`). Store при пустом текущем снимке возвращает `(0, nil)` (`internal/mihomonative/store.go:928-929`). Более того, тест намеренно закрепляет этот no-op как правильное поведение (`internal/mihomonative/rules_parity_test.go:216-220`).

Это расходится с утверждённым контрактом: пустой набор удаления должен быть ошибкой клиента, а не ответом `deleted=true, deletedCount=0`.

Исправление:

1. Handler: `len(ids) == 0` -> HTTP 400 `INVALID_REQUEST`.
2. Store: защитная проверка пустого списка -> `ErrSelectionMismatch` (нельзя полагаться только на handler).
3. Заменить no-op тест на отрицательные store/API-тесты.
4. Уточнить OpenAPI: `ids` обязательно, `minItems: 1`, элементы непустые, `uniqueItems: true`.

### P1 — acceptance bootstrap удаляет существующий cache и не защищён от гонок

Bootstrap создаёт staging по PID, а при публикации без блокировки выполняет `rm -rf "$CACHE_DIR"` и затем `mv` (`scripts/bootstrap-mihomo-acceptance.sh:52-56,98-101`). Два параллельных процесса могут одновременно скачать артефакты, удалить каталог друг друга или заменить уже корректный cache. Повреждённый cache также уничтожается автоматически вместо безопасной публикации нового поколения с сохранением диагностируемого состояния.

Дополнительно значения manifest превращаются в shell-код через `eval` (`bootstrap-mihomo-acceptance.sh:14-28`). Даже если текущий manifest доверенный, это хрупкий и необязательный канал инъекции/ошибок quoting.

Исправление:

1. Ввести межпроцессный lock на конкретный `<version>/<manifest-sha>` (`flock` либо атомарный lock-dir с cleanup).
2. После получения lock повторно проверить готовый cache.
3. Staging создавать через `mktemp -d` внутри cache parent.
4. Публиковать только полностью проверенный staging атомарным rename; существующий повреждённый target не удалять молча — переименовать в quarantine с уникальным именем или завершиться с понятной ошибкой.
5. Убрать `eval`: передавать значения из Python в NUL-delimited/JSON output и читать безопасно либо выполнять bootstrap-логику в одном языке.
6. Добавить тест двух конкурентных bootstrap-процессов и тест повреждённого completion marker/cache.

### P1 — acceptance runner зависит от текущего каталога вызова

Runner вычисляет `REPO_ROOT`, но перед `go test ./internal/singbox/router` не выполняет `cd "$REPO_ROOT"` (`scripts/run-mihomo-acceptance.sh:4,29-34`). Поэтому абсолютный запуск скрипта из домашнего или временного каталога завершится не по заявленному воспроизводимому сценарию.

Исправление: после определения `REPO_ROOT` выполнить `cd "$REPO_ROOT"` и добавить smoke-test запуска runner из `/tmp`.

### P1 — DNS parser принимает `system://anything`

Ветка `case "system"` игнорирует содержимое `rest` и всегда возвращает `system://` (`internal/mihomo/dns_parser.go:92-97`). Некорректная пользовательская строка молча меняет смысл вместо fail-closed ошибки.

Исправление: разрешать только точное `system`/`system://`; для `system://` требовать пустой `rest`, иначе возвращать compile error. Добавить отрицательные тесты `system://anything`, `system:///path`, `system://:53`.

### P2 — bracketed адрес с пустым портом принят как корректный

Для URI и plain address `[IPv6]:` устанавливает `portStr == ""`, после чего проверка порта пропускается (`internal/mihomo/dns_parser.go:139-170,184-210`). Наличие двоеточия должно означать обязательный непустой порт.

Исправление: отдельно хранить `portSpecified`; если разделитель `:` присутствовал, требовать порт `1..65535`. Покрыть `[::1]:`, `udp://[::1]:`, `https://[::1]:/dns-query`.

### P2 — acceptance version check использует substring

`validateMihomoVersion` проверяет `strings.Contains(output, expected)`. Это допускает ложное совпадение вроде ожидаемого `1.19.29` внутри `11.19.290`. SHA pin снижает текущий риск, но validator должен проверять выделенный version token точно.

Исправление: распарсить token после `Mihomo Meta v`/`Mihomo v` и сравнить нормализованные версии на равенство; добавить near-match negative test.

## Что подтверждено

- Exact arity composite child tokens и registry rule types присутствуют.
- `RuleInput.Enabled *bool` и разделение create/update присутствуют.
- Revision snapshot и rollback in-memory при ошибке сохранения реализованы.
- Windows replacement использует `MoveFileEx(..., MOVEFILE_REPLACE_EXISTING)` без предварительного удаления destination.
- Unsupported-rules UI, typed client и OpenAPI endpoints присутствуют.
- `git diff --check` завершился кодом 0; присутствуют только предупреждения о будущей нормализации CRLF/LF.
- Текущий запуск `go test ./internal/mihomo ./internal/mihomonative ./internal/singbox/router ./internal/api` в WSL завершился кодом 0.

Прохождение существующих тестов не закрывает перечисленные дефекты: duplicate-ID case отсутствует, а пустой запрос закреплён тестом как допустимый.

## Обязательный порядок исправления

1. Закрыть P0: exact set equality и duplicate-ID rejection.
2. Закрыть empty-ID contract в store, handler, OpenAPI и тестах.
3. Сделать bootstrap concurrency-safe и убрать `eval`/безусловное удаление target cache.
4. Сделать runner независимым от cwd.
5. Закрыть DNS fail-open случаи и exact version parsing.
6. Повторно сгенерировать OpenAPI и проверить двухпроходную идемпотентность.

## Критерии повторной приёмки

Обязательны все пункты:

1. Store-test: `[A,B]` против `[A,A]` возвращает mismatch, ничего не удалено.
2. API-test: duplicate IDs -> 400; empty/missing/null IDs -> 400; состояние не изменено.
3. OpenAPI описывает `minItems: 1` и `uniqueItems: true`.
4. DNS negative corpus отклоняет `system://anything` и все формы с указанным, но пустым портом.
5. Bootstrap fault/concurrency tests доказывают отсутствие удаления валидного cache, частичной публикации и взаимного повреждения.
6. Runner успешно запускается по абсолютному пути из каталога вне репозитория и машинно подтверждает run/pass/no-skip.
7. Повторно проходят:
   - `go test ./internal/mihomo ./internal/mihomonative ./internal/singbox/router ./internal/api`
   - `go test -race ./internal/mihomonative ./internal/api`
   - acceptance runner;
   - frontend Vitest;
   - `npm run check`;
   - двухпроходная OpenAPI idempotence;
   - `git diff --check`.

## Ограничения этой проверки

IPK не собирался, на роутеры ничего не устанавливалось. Acceptance bootstrap повторно не запускался, чтобы аудит не скачивал артефакты и не модифицировал cache. Полный frontend build не повторялся; вывод walkthrough о его предыдущем прохождении не является независимо подтверждённым этим аудитом.
