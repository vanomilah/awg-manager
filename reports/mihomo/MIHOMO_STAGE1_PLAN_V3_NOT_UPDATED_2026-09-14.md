# Mihomo Stage 1 Plan v3 — изменения не внесены

Дата проверки: 2026-09-14  
Проверен файл: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

Файл по-прежнему содержит редакцию **v3** на 222 строки. Две обязательные поправки из `MIHOMO_STAGE1_REMEDIATION_PLAN_V3_FINAL_RECHECK_2026-09-14.md` в него не внесены.

## Что осталось без изменений

### 1. Legacy rules всё ещё фильтруются молча

В плане по-прежнему записано:

```text
Store.ConfigRules() filters out unsupported rules so compiler/runtime is not broken, logging a warning.
```

и повторно:

```text
ConfigRules() []string: omits unsupported rules from compiler input, logging a warning.
```

Это неприемлемо, потому что текущий `Store` не имеет logger, а `ConfigRules()` не возвращает ошибку. Активное правило будет видно пользователю, но не попадёт в runtime.

Требуемая замена:

- добавить `ValidateRuntimeRules() error` и вызывать её в router adapter до `ConfigRules()`;
- наличие активного unsupported rule должно блокировать apply fail-closed;
- `ListRules()` оставить без изменения;
- `DeleteUnsupportedRules()` не вызывать автоматически;
- удаление выполнять только после явного подтверждения пользователя.

Альтернатива: изменить `ConfigRules()` на `([]string, error)` и обновить интерфейс `MihomoNativeProxySource`, router adapter, mocks и тесты.

### 2. Обычный тест всё ещё должен изменять рабочее дерево

В плане по-прежнему записано:

```text
rules_manifest_test.go generates frontend/src/lib/types/mihomoRuleTypes.generated.ts
```

Требуемая замена:

- отдельный generator (`go generate` или `internal/mihomo/cmd/genrules`);
- generator создаёт deterministic `mihomoRuleTypes.generated.ts`;
- `rules_manifest_test.go` ничего не записывает, а только сравнивает ожидаемые bytes с committed-файлом;
- verification запускает generator и проверяет отсутствие неожиданного diff;
- generated-файл содержит `Code generated ... DO NOT EDIT.`.

## Решение

План пока нельзя считать окончательно исправленным. Агенту нужно внести две замены выше. Все остальные архитектурные пункты v3 ранее одобрены и повторного пересмотра не требуют.

