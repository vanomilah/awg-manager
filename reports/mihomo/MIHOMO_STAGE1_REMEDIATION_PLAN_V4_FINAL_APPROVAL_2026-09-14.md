# Final Approval: Mihomo Stage 1 Remediation Plan v4

Дата: 2026-09-14  
Проверен файл: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Редакция: v4, 242 строки.

## Вердикт

**План v4 одобрен для реализации с четырьмя точечными обязательными поправками, перечисленными ниже.**

Предыдущие блокеры устранены правильно:

- unsupported enabled rules блокируют генерацию fail-closed;
- `ListRules()` не меняет сигнатуру;
- legacy rules не удаляются автоматически;
- generator отделён от read-only parity test;
- DNS normalization стала fallible;
- frontend build и hermetic binary validation включены в приёмку.

Нового архитектурного цикла планирования не требуется. Исполнитель может начинать работу, включив следующие поправки в реализацию.

## Обязательные точечные поправки

### 1. Обновить интерфейс `MihomoNativeProxySource` и все test doubles

Сейчас `internal/singbox/router/service.go` содержит интерфейс `MihomoNativeProxySource`, в котором нет `ValidateRuntimeRules()`.

Поскольку `service_mihomo.go` работает через этот интерфейс, план обязан добавить:

```go
type MihomoNativeProxySource interface {
    ValidateRuntimeRules() error
    // существующие методы без изменений
}
```

Необходимо обновить все fake/mock/stub реализации интерфейса и соответствующие compile-time assertions. Иначе реализация пункта 161–162 плана не соберётся.

### 2. `DeleteUnsupportedRules` должен возвращать ошибку сохранения

Запланированная сигнатура:

```go
DeleteUnsupportedRules() int
```

не позволяет сообщить ошибку `saveLocked()`. Для файлового store это нарушает транзакционность: UI может получить количество удалённых правил, хотя файл не сохранён.

Использовать:

```go
func (s *Store) DeleteUnsupportedRules() (int, error)
```

Контракт:

1. Под lock построить новый список и сохранить старый.
2. Если unsupported rules нет — вернуть `(0, nil)` без записи.
3. Назначить новый список и вызвать `saveLocked()`.
4. При ошибке восстановить старый список и вернуть `(0, err)`.
5. Только после успешного сохранения вернуть фактическое число удалённых правил.

Добавить failure-injection тест сохранения и проверку rollback in-memory state.

### 3. Либо добавить реальный UI/API migration action, либо убрать обещание о нём

План говорит, что legacy rules удаляются пользователем через UI, однако в перечне изменений нет:

- HTTP endpoint для `UnsupportedRules()`;
- endpoint для подтверждённого удаления;
- API client/schema;
- UI banner/card с ID и type проблемных правил;
- подтверждения необратимого удаления.

Предпочтительно добавить минимальный migration surface:

- read endpoint возвращает unsupported rules;
- delete endpoint требует явный запрос пользователя;
- UI показывает блокирующую причину и предлагает «Изменить» или «Удалить неподдерживаемые»;
- после удаления конфигурация применяется только отдельным обычным действием, не автоматически.

Если UI/API не входит в Stage 1, удалить из плана утверждение «deleted or edited upon explicit user action in UI» и оставить только сохранение данных плюс fail-closed ошибку. Но тогда walkthrough обязан отметить, что восстановление выполняется вручную через persisted store.

### 4. Проверка generator idempotence должна быть scoped к generated-файлу

Команда `git status -s` не может служить критерием «нет dirty files»: текущая feature-ветка уже содержит большое количество несвязанных изменённых и untracked файлов.

Использовать проверку до/после только для generated artifact, например:

1. Вычислить hash `mihomoRuleTypes.generated.ts` до запуска generator.
2. Запустить generator.
3. Сравнить hash после запуска; повторный запуск не должен менять файл.
4. Выполнить scoped diff:

```powershell
git diff --exit-code -- frontend/src/lib/types/mihomoRuleTypes.generated.ts
```

Если файл ещё untracked в рабочем дереве, сравнивать bytes/hash двух последовательных запусков и зафиксировать generated-файл как часть реализации. Нельзя требовать чистоты всего рабочего дерева и нельзя удалять посторонние пользовательские файлы ради проверки.

## Рекомендации для generator

- Вынести единый renderer, например `RenderRuleTypesTypeScript() []byte`, и использовать его и CLI generator, и read-only test. Не дублировать форматирование в двух местах.
- Сортировка categories/types должна быть детерминированной и явно закреплённой.
- CLI должен находить repository root надёжно либо принимать output path; `go generate` запускается с рабочей директорией пакета, а `go run ./internal/mihomo/cmd/genrules` — обычно из корня.
- Запись generated-файла выполнять атомарно: временный файл рядом, затем rename.
- Read-only test не должен создавать директории, временные файлы внутри репозитория или менять mtime generated artifact.

## Дополнение к тестам

К уже перечисленным в плане тестам добавить:

- compile-time проверку, что `*mihomonative.Store` реализует обновлённый `MihomoNativeProxySource`;
- router mock с `ValidateRuntimeRules()` returning nil/error;
- `DeleteUnsupportedRules`: no-op, successful delete, disk-write failure with rollback;
- disabled unsupported rule не блокирует runtime, но остаётся доступным через `UnsupportedRules()`;
- повторный generator run создаёт идентичные bytes;
- read-only parity test не меняет hash/mtime файла.

## Условия итоговой приёмки

Реализацию можно принять, если:

1. Все команды backend/frontend из v4 проходят.
2. Representative `mihomo -t` выполнен, а не пропущен в финальной приёмке.
3. Unsupported active rule действительно останавливает apply до записи runtime config.
4. Ошибка удаления persisted rules не приводит к изменению in-memory state.
5. Generator и тесты не изменяют постороннее dirty working tree.
6. Walkthrough содержит точные команды, результаты и список изменённых файлов.
7. IPK не собирается и deploy не выполняется в рамках этой стадии.

## Итог для исполнителя

Начинать реализацию можно. Считать четыре пункта выше частью плана v4, даже если исходный `implementation_plan.md` ещё не переписан. После выполнения нужен аудит фактического кода и `walkthrough.md`, а не очередной теоретический пересмотр плана.

