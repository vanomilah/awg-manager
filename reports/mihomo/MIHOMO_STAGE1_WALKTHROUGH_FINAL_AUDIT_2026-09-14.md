# Аудит walkthrough Mihomo Stage 1

Дата: 2026-09-14  
Проверяемый документ: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`  
Репозиторий: `E:\AWGM\awg-manager`  
Ветка: `feature/mihomo-ai-proxyrt`

## Вердикт

Утверждение walkthrough «Stage 1 COMPLETE and ACCEPTED» **пока не подтверждено**. Основная архитектура Stage 1 действительно реализована, а выбранные Go-пакеты проходят тесты, но обнаружены два существенных дефекта и несколько расхождений между отчётом и фактической реализацией.

До исправления P1 ниже Stage 1 следует считать **условно реализованным, но не принятым**.

## Найденные проблемы

### P1 — бинарная проверка не герметична и может быть молча пропущена

Walkthrough утверждает, что официальный бинарник Mihomo и geodata находятся в тестовых fixtures, а проверка полностью автономна. Код этого не подтверждает:

- `internal/singbox/router/service_mihomo_test.go:811-824` ищет бинарник через `MIHOMO_BIN`, `PATH` или `~/.local/bin/mihomo`; если его нет, тест вызывает `t.Skip`.
- `internal/singbox/router/service_mihomo_test.go:829-837` копирует geodata из `~/.local/share/mihomo`, то есть из внешнего состояния конкретной машины.
- ошибки чтения и записи geodata игнорируются;
- зафиксированных test fixtures, версии бинарника, checksum и обязательной CI-задачи не найдено;
- каталог `prebuilt/mihomo/` существует как untracked, но тест его не использует.

Следствие: зелёный `go test` не доказывает совместимость с реальным Mihomo. На чистом CI тест может быть пропущен и весь набор всё равно завершится успешно.

**Что исправить:**

1. Сделать два явно разделённых режима:
   - unit/integration suite без внешнего бинарника;
   - обязательный acceptance job с закреплённой версией и SHA-256 Mihomo/geodata.
2. В acceptance job отсутствие бинарника или fixtures должно быть ошибкой, не `Skip`.
3. Fixtures должны приходить из контролируемого CI cache/artifact либо документированного bootstrap-скрипта с проверкой checksum.
4. Не игнорировать ошибки `ReadFile`/`WriteFile`.
5. В выводе теста печатать версию реально запущенного бинарника.

### P1 — вложенный composite rule принимает лишние поля

В `internal/mihomo/config.go:916-922` для вложенных `AND`, `OR`, `NOT` проверяется только `len(childTokens) < 2`, после чего рекурсивно разбирается исключительно `childTokens[1]`. Лишние токены вложенного правила не отклоняются.

Например, конструкция вида:

```text
AND,((NOT,((DOMAIN,example.com)),unexpected),(NETWORK,tcp)),DIRECT
```

может пройти внутренний парсер, потому что `unexpected` не участвует в рекурсивной проверке. Это противоречит заявлению об exact arity и fail-closed grammar.

**Что исправить:** для вложенных composite rules требовать точное количество полей, предусмотренное registry (для текущего синтаксиса — ровно type + payload), и добавить отрицательные тесты для лишнего target/modifier/trailing token на каждом уровне вложенности.

### P1 — migration API не доведён до пользовательского сценария

Backend-маршруты существуют (`internal/api/mihomo_handler.go:230-233`, `347-364`), однако во frontend и OpenAPI не найдено обращений к `rules/unsupported`.

Фактически пользователь не получает обещанного штатного действия миграции. POST удаляет все неподдерживаемые правила сразу и не требует тела с подтверждением/ожидаемым списком ID. `apply` по умолчанию выключен, но сохранённые данные всё равно удаляются.

**Что исправить:**

- добавить предупреждение в UI со списком правил и явным подтверждением удаления;
- передавать IDs или revision/confirmation token, чтобы не удалить правила, изменившиеся после GET;
- добавить API в OpenAPI и типизированный frontend client;
- проверить авторизацию, stale revision, частичный конфликт, rollback save/apply и повторный запрос;
- оставить один канонический путь API, а alias документировать как compatibility route либо удалить.

### P2 — DNS-функция валидирует fragment, но не сам DNS endpoint

`internal/mihomo/dns_parser.go:57-64` возвращает любую непустую строку без `#` без проверки. При наличии `#` проверяется непустой base, но не его IP, host:port, URI scheme или URL grammar.

Поэтому имя `NormalizeAndValidateDNSServerURI` и формулировка walkthrough создают более сильную гарантию, чем код. Строка наподобие `not a dns endpoint` принимается.

**Что исправить:** либо реализовать строгий разбор поддерживаемых Mihomo endpoint forms, либо переименовать функцию/документацию в нормализатор fragment и явно оставить окончательную проверку бинарнику. Добавить отрицательные тесты base URI.

### P2 — неизвестный listener type обрабатывается fail-open

`internal/mihomo/config.go:961-970` неизвестный тип listener получает TCP capability вместо ошибки. Это расходится с общей политикой fail-closed и способно скрыть неверный тип при проверке конфликтов.

**Что исправить:** registry допустимых listener types с явной ошибкой для неизвестного типа либо документированный conservative fallback с отдельным тестом. Предпочтительно отклонять неизвестный тип до генерации.

### P2 — невозможно штатно сохранить выключенное правило

`internal/mihomonative/store.go:815-816` принудительно меняет `Enabled=false` на `true`. В результате boolean не позволяет различить «поле не задано» и явное выключение. Это осложняет миграцию: runtime validation пропускает выключенные legacy rules, но обычный SaveRule не способен сохранить такое состояние.

**Что исправить:** использовать `*bool`, отдельную DTO или явный default только при create; при update сохранять переданное `false`. Добавить create/update round-trip tests.

### P3 — генератор не гарантированно переносим на Windows

`internal/mihomo/cmd/genrules/main.go:53` делает `os.Rename(tmpFile, target)` поверх существующего файла. Такое замещение имеет платформенные различия и может завершиться ошибкой на Windows.

**Что исправить:** применить общий atomic-write helper проекта либо покрыть Windows-compatible replacement тестом.

## Что подтверждено положительно

- Канонический registry и генерация TypeScript действительно добавлены.
- UI использует generated список поддерживаемых типов.
- `SUB-RULE` исключён из registry/UI и явно отклоняется в backend.
- `ValidateRuntimeRules()` вызывается перед получением runtime rules.
- Store откатывает in-memory slice при ошибке сохранения удаления unsupported rules.
- Ошибки конфигурации санитизируются rune-safe.
- Проверка provider `proxy` reference и listener collisions присутствует.
- Read-only parity test не переписывает tracked файл.

## Независимая проверка

Успешно выполнено ранее в этом аудите:

```text
go test ./internal/mihomo ./internal/mihomonative ./internal/singbox/router ./internal/api
```

Все четыре пакета прошли. Отдельно прошёл `TestGenerateMihomoConfig_RepresentativeBinaryValidation`, однако он использовал внешние файлы из WSL home, поэтому это не подтверждает герметичность.

`git diff --check` не выявил whitespace errors; присутствуют многочисленные предупреждения CRLF/LF.

Повторный запуск WSL-проверки в конце аудита не состоялся из-за `Wsl/Service/E_ACCESSDENIED`. Frontend-команда `npm test ... && npm run check` ранее завершилась по timeout без вывода. Поэтому заявления walkthrough о текущих frontend check/build не были независимо воспроизведены в этой проверке.

Рабочее дерево содержит очень большое количество изменённых и untracked файлов. Это не позволяет связывать успешные тесты исключительно со Stage 1 без изоляции diff/commit.

## Обязательные критерии повторной приёмки

1. Исправлен exact arity вложенных composite rules и добавлены отрицательные тесты.
2. Acceptance test Mihomo не зависит от `$HOME`, не игнорирует I/O ошибки и не может тихо перейти в `Skip` в обязательной CI-задаче.
3. Migration flow доступен через UI и типизированный API, требует явного подтверждения и защищён от stale list.
4. Определён и протестирован контракт DNS base validation.
5. Неизвестные listener types отклоняются fail-closed.
6. Повторно проходят Go suites, frontend Vitest, `svelte-check`, production build, generator parity и `git diff --check` в чистом checkout.
7. В итоговом walkthrough отдельно перечислены фактически выполненные команды, skipped tests и источник бинарника/fixtures.

## Рекомендация следующему агенту

Не начинать Stage 2 до закрытия первых трёх пунктов P1. Сначала добавить регрессионный тест на trailing token во вложенном composite rule, затем переделать binary acceptance harness, после этого завершить migration UX/API. Остальные P2 можно закрывать тем же remediation-пакетом.
