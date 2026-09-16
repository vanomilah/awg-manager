# Финальное одобрение Remediation Plan v6.2

Дата: 2026-09-15  
Проверенный план: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

План v6.2 **одобрен к реализации**. Три блокирующих замечания v6.1 устранены:

- offline bootstrap tests используют отдельный `MIHOMO_ACCEPTANCE_CACHE_ROOT`;
- download/checksum failures и download-before-quarantine разложены на воспроизводимые сценарии;
- runner и self-tests используют единственный `verify-mihomo-acceptance-json.py`.

Также добавлены динамический loopback port, HTTP request counter, restart-persistence checks, generated OpenAPI assertions и расширенная проверка версии.

Новая архитектурная редакция плана не нужна. Ниже приведены небольшие обязательные уточнения, которые агент должен соблюдать непосредственно при реализации и отразить в walkthrough.

## Уточнения исполнения

### 1. Не называть предложенный regex полной SemVer 2.0 валидацией

Выражение:

```text
[0-9]+(?:\.[0-9]+)+...
```

разрешает не только `MAJOR.MINOR.PATCH`, но и `1.2`, `1.2.3.4`, ведущие нули и некоторые некорректные prerelease-последовательности. Для текущей задачи достаточно точного сравнения pin из manifest с token из вывода Mihomo, но заявление «Full SemVer 2.0» будет неточным.

При реализации выбрать один вариант:

1. предпочтительно — использовать точную SemVer grammar/library и требовать ровно `MAJOR.MINOR.PATCH`; либо
2. назвать функцию `extractMihomoVersionToken`, ограничить grammar ожидаемыми форматами Mihomo и не заявлять полноценную SemVer-валидацию.

Обязательны отрицательные тесты `1.19`, `1.19.29.1`, `01.19.29`, пустого prerelease/build identifier и нескольких конфликтующих Mihomo token.

### 2. Cache isolation проверять содержимым, а не timestamp каталогов

Проверка «repository cache 100% identical» должна сравнивать относительные пути, типы, размеры и SHA-256 файлов. Не следует считать изменившиеся directory timestamps нарушением. До запуска test harness необходимо resolve/canonicalize оба пути и доказать, что временный `CACHE_ROOT` не равен и не вложен в production repository cache.

### 3. HTTP test server и фоновые процессы должны всегда завершаться

Integration script обязан сразу после создания temp root зарегистрировать trap, который:

- завершает только сохранённый PID собственного HTTP server;
- дожидается его через `wait`;
- завершает/дожидается обоих bootstrap children при аварийном выходе;
- очищает только canonicalized test temp root;
- не трогает repository cache и чужие `staging.*`.

Endpoint `/request-count` не должен увеличивать счётчик скачиваний при чтении самого счётчика. Надёжнее хранить request log в файле test root и считать только запросы к fixture paths.

### 4. Проверять доступность обязательных инструментов fail-closed

В начале bootstrap/test harness явно проверить `python3`, `sha256sum`, `curl`, `gzip`, `mktemp`, `flock`. При отсутствии `flock` production bootstrap должен завершаться с понятной ошибкой, а не продолжать без блокировки.

### 5. Quarantine publication должна сохранять возможность восстановления

После полной проверки staging и перемещения повреждённого target в quarantine публикация нового target может упасть. В этом случае:

- bootstrap возвращает non-zero;
- quarantine не удаляется;
- ошибка сообщает пути staging/quarantine или гарантирует безопасную автоматическую попытку вернуть старый каталог;
- test покрывает отказ финального `mv`/publication настолько, насколько это возможно через injected filesystem operation.

Отсутствующий target в коротком интервале между двумя rename допустим для повреждённого cache, но частично заполненный target недопустим.

### 6. OpenAPI считается исправленным только после assertion

Go struct tags из плана являются предложением, а не гарантией поддержки текущей версией swaggo. Если generator их игнорирует, агент обязан найти поддерживаемый генератором способ. Ручная правка двух YAML без устойчивого source-of-truth не принимается. Финальный `verify-openapi-unsupported-rules.py` и двухпроходный generation test должны пройти.

## Обязательный walkthrough после реализации

Итоговый отчёт должен содержать:

1. список реально изменённых файлов;
2. точные команды и exit codes каждой проверки;
3. отдельный результат P0 duplicate-ID regression test;
4. подтверждение reload-from-disk после каждого destructive error case;
5. лог всех offline bootstrap scenarios, включая два параллельных процесса;
6. доказательство неизменности production cache;
7. результат verifier self-tests;
8. фактический fragment сгенерированного OpenAPI с `minItems`, `uniqueItems`, `minLength`;
9. acceptance run из `/tmp` с `run + pass`, без `skip/fail`;
10. честный список непроведённых проверок, если какая-либо команда не завершилась.

## Границы работы

- Не собирать IPK.
- Не выполнять деплой на роутеры.
- Не использовать `--force-reinstall`.
- Не запускать cleanup.
- Не изменять посторонние файлы грязной рабочей копии.
- Не удалять существующие cache/quarantine каталоги автоматически.

## Решение для агента

**Приступать к реализации v6.2**, соблюдая шесть уточнений этого файла. После реализации предоставить walkthrough для повторного code review; одних заявлений о прохождении тестов недостаточно — требуются фактические результаты и проверка кода.
