# Final approval: Mihomo Stage 1 Remediation Plan v5.3

Дата: 2026-09-14  
Проверенный файл: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

План v5.3 **одобрен для реализации**. Все архитектурные требования предыдущих аудитов включены: immutable upstream tags, строгий acceptance mode, разделение REST create/update, полный revision snapshot, отсутствие nested lock, сохранение compatibility batch API, scheme-specific DNS validation, UI подтверждения и OpenAPI contract.

Новую версию плана составлять не требуется. Агент может начинать реализацию, но обязан учесть перечисленные ниже execution corrections. Они не меняют архитектуру, однако без них отдельные verification-команды или fallback paths работать не будут.

## Обязательные execution corrections

### 1. Исправить fallback path к manifest

Тест расположен в:

```text
internal/singbox/router/service_mihomo_test.go
```

Путь к корню репозитория от каталога файла проходит через три уровня. Поэтому fallback должен разрешаться как:

```text
../../../scripts/mihomo-acceptance-manifest.json
```

а не `../../scripts/...`. Лучше вычислить абсолютный путь через `runtime.Caller`, `filepath.Dir`, `filepath.Join` и затем `filepath.Abs`, после чего проверить, что файл существует.

Основной acceptance flow всё равно должен использовать обязательный абсолютный `MIHOMO_ACCEPTANCE_MANIFEST`; fallback допустим только для удобства локального запуска и должен быть отдельно протестирован.

### 2. No-skip verification нужно выполнять целиком внутри WSL

Команда из плана закрывает кавычки `bash -lc` перед pipe и запускает `python3` уже на стороне Windows PowerShell. Кроме того, `\$PWD` не является надёжным экранированием переменной PowerShell, а pipeline может скрыть исходный exit code `go test`.

Не собирать эту проверку длинной межоболочечной строкой. Добавить versioned скрипт:

```text
scripts/run-mihomo-acceptance.sh
```

Скрипт должен работать с `set -euo pipefail`, сам определить repo root, задать абсолютные fixture/manifest paths, сохранить `go test -json` во временный файл, сохранить exit code теста и затем отдельным Python/Go helper проверить события `run`, отсутствие `skip`, наличие `pass`. Временный файл удаляется через `trap`.

Из PowerShell запускается только:

```powershell
wsl.exe -d Ubuntu -- bash /mnt/e/AWGM/awg-manager/scripts/run-mihomo-acceptance.sh
```

Проверка обязана завершиться ошибкой и при ненулевом exit code `go test`, и при `skip`, и при отсутствии события целевого теста.

### 3. Проверить checksum до фиксации результата

Tag `Loyalsoldier/v2ray-rules-dat:202609132350` существует и является immutable release tag. Однако сами значения SHA-256 в ходе этого ревью независимо не подтверждены.

Bootstrap должен считать digest загруженных файлов и сравнить его с manifest. В walkthrough необходимо вывести:

- окончательный URL после redirect;
- asset name и release tag;
- ожидаемый и фактический SHA-256;
- размер compressed/uncompressed файла;
- версию бинарника.

Если хотя бы один digest не совпал, не подбирать новый checksum автоматически. Остановить реализацию данного шага, сверить digest с официальным release metadata и только затем осознанно обновить manifest.

### 4. Безопасно публиковать cache при существующем повреждённом каталоге

Простой `mv staging .cache/mihomo/v1.19.29/` некорректен, если target уже существует: staging может оказаться вложенным каталогом или операция завершится ошибкой.

Использовать immutable directory, включающий digest manifest, например:

```text
.cache/mihomo/v1.19.29/<manifest-sha256>/
```

Тогда новый staging публикуется в ещё не существующий target. Completion marker создаётся последним. Повреждённый существующий target не удалять автоматически; создать новый digest path или завершиться с понятной ошибкой.

### 5. Сохранить старый batch contract отдельно от REST defaults

Правильно, что `SaveRulesBatch([]Rule)` сохранён для `trafficSvc`. При реализации:

- убрать из compatibility path старое принудительное `Enabled=true`;
- сохранить точные persisted значения всех правил;
- REST create/update должны использовать только `RuleInput`;
- проверить `MihomoHandler.BatchSaveRules` и все найденные `SaveRule`/`SaveRulesBatch` callers;
- не превращать compatibility batch в implicit REST upsert;
- добавить тест, что batch с `Enabled=false` остаётся выключенным после записи и reload с диска.

### 6. Конкретизировать Windows replace во время реализации

План правильно запрещает предварительное удаление target, но `atomic replace` пока назван абстрактно. Реализация должна иметь platform-specific проверенный механизм либо простой безопасный write contract для build-time generator. Существующий `storage.AtomicWrite` сам использует `os.Rename` и не является автоматическим доказательством Windows compatibility.

Обязательны Windows tests: замена существующего target, сохранение старого файла при ошибке, cleanup temp и отсутствие окна предварительного удаления.

## Дополнения к acceptance tests

В уже запланированный набор добавить:

1. test fallback manifest path;
2. test пустого `MIHOMO_FIXTURE_DIR` в acceptance mode;
3. test manifest schema/version и неизвестных полей;
4. test corrupted completion marker/cache;
5. test all DNS corpus values через реальный `mihomo -t`;
6. test revision stability и изменения каждого включённого поля;
7. test duplicate rule IDs в persisted legacy data;
8. API auth tests и повторный POST после успешного удаления;
9. frontend test 409 -> reload -> обязательное повторное подтверждение;
10. `go test -race ./internal/mihomonative ./internal/api` на Linux.

## Условия приёмки walkthrough

Реализацию можно считать закрытой только если итоговый walkthrough содержит:

- scoped список изменённых файлов;
- фактические команды и exit codes;
- отдельный результат no-skip acceptance script;
- версию и digest реально запущенного Mihomo;
- точные immutable geodata tags/digests;
- результаты Go, race, frontend, Svelte, build, OpenAPI two-pass и generator parity;
- список всех skipped tests;
- `git diff --check`;
- подтверждение, что IPK и деплой не выполнялись, если пользователь отдельно этого не запросил.

## Итоговое решение

**APPROVED FOR IMPLEMENTATION WITH EXECUTION CORRECTIONS.**

Другому агенту следует реализовывать v5.3 непосредственно сейчас. Повторно переписывать весь план не нужно; шесть corrections выше должны быть внесены прямо в ходе работы и отражены в walkthrough.
