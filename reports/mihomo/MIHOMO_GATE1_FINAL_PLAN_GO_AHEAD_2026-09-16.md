# Финальный допуск плана Mihomo Gate 1

Дата: 2026-09-16  
Проверенный файл: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

План **можно запускать в реализацию на Gemini Pro**. Основные архитектурные требования приняты: descriptor-relative filesystem, сериализация writer, immutable CAS, поколения до destructive swap, отдельный RuntimeOff, per-operation bridge journal, roll-forward после commit intent, protected GC и Linux crash/failpoint tests.

Переписывать план еще раз целиком не требуется. Однако следующие условия являются обязательными implementation notes и acceptance blockers.

## Обязательные предохранители реализации

### 1. Advisory lock и transaction ownership — разные вещи

Постоянный файл `transaction.lock` сам по себе не представляет ownership. Ownership существует только пока ядро удерживает `flock`/`fcntl` lock на открытом fd; после crash ядро автоматически освобождает блокировку, хотя файл остается.

В реализации необходимо:

- выбрать один механизм (`flock` либо `fcntl`) и использовать его во всех writer/recovery entry points;
- открывать fd с `CLOEXEC`;
- не удалять lock-файл;
- определить timeout/nonblocking policy;
- удерживать lock на всю coordinator transaction/recovery operation либо строго описать безопасные точки освобождения;
- хранить диагностический owner metadata отдельно и никогда не считать его доказательством живого lock holder;
- проверить отдельным процессным тестом освобождение lock после аварийного завершения владельца.

### 2. Поврежденный manifest нельзя «перевести» обычным CAS

Если JSON не декодируется или schema version неизвестна, из него невозможно безопасно получить current `(TxID, Sequence, State)` и записать следующий state.

Правильное поведение:

- остановить новые apply-операции;
- сохранить поврежденный файл как forensic evidence либо оставить неизменным;
- выставить отдельный observable recovery fault/sidecar marker;
- потребовать детерминированную recovery/manual repair процедуру;
- не перезаписывать поврежденный manifest новым `recovery_required` manifest поверх улик.

### 3. Другой TxID не принадлежит новому caller

При обнаружении валидного незавершенного manifest с другим TxID новый запрос не должен переписывать его в `recovery_required` от своего имени. Он должен отказаться от старта и вызвать/ожидать recovery существующей транзакции под межпроцессным lock.

### 4. `recovery_required -> Any Intent/Applied` запрещено

Формулировку `[Any Intent/Applied state]` нельзя реализовать как свободный переход. Recovery resolver должен по durable evidence выбрать ровно одно допустимое действие:

- повторить идемпотентную apply/verify операцию;
- продолжить rollback до commit boundary;
- продолжить roll-forward после commit intent;
- остаться в recovery required при неоднозначности.

Каждая ветка должна иметь отдельный тест. Оператор или входной запрос не может произвольно выбрать state.

### 5. Исправить terminal cleanup симметрию

В таблице `abort_in_progress -> idle`, тогда как rollback идет через `terminal_cleanup`. Abort также обязан пройти durable cleanup journal:

```text
abort_in_progress -> terminal_cleanup -> idle
```

Прямой переход в idle разрешать нельзя, если существуют snapshot, candidate bundle, temp dirs или bridge operation artifacts.

### 6. Не заявлять атомарность двух независимых файлов

В RuntimeOff шаг «atomically update verified-active and generation pointer» технически неверен. Это две последовательные durable записи под `commit_intent`, а согласованность обеспечивается roll-forward recovery protocol из Phase F. В коде и комментариях не называть их общей атомарной транзакцией.

### 7. Runtime verification не ограничивать `/proc`

Наличие/отсутствие PID подтверждает состояние процесса, но не готовность движка. Для RuntimeOn нужны как минимум:

- PID identity/start time или pidfd, чтобы исключить PID reuse;
- проверка ожидаемого listener/API;
- проверка загруженной generation/config digest, если интерфейс ядра это позволяет;
- bounded readiness timeout.

Для RuntimeOff необходимо подтвердить отсутствие именно нужного процесса и закрытие принадлежащих ему listener ports.

### 8. Fallback `openat2`

Fallback на `openat` допустим только для ошибок, означающих отсутствие поддержки syscall/flags (`ENOSYS`, обоснованный `EINVAL`). Ошибки безопасности (`EXDEV`, `ELOOP`, `EPERM` и подобные) нельзя превращать в fallback, иначе защита может быть понижена после обнаруженной атаки.

## Модель выполнения

### Сейчас

Оставить на **Gemini Pro**:

- Phases A–G;
- реализацию lock/CAS/state machine;
- strictfs Linux;
- commit/recovery/rollback;
- RuntimeOn/RuntimeOff;
- bridge reconciliation;
- базовые и конкурентные acceptance tests.

### Точка возможного переключения на Flash

Вернуться на ревью после того, как Pro предоставит `walkthrough.md`, фактический diff и зеленые результаты:

```text
go test -count=1 ./internal/strictfs ./internal/mihomo
go test -race -count=1 ./internal/mihomo
git diff --check
```

Причем первые две команды должны быть подтверждены на Linux. После этого Flash можно использовать для расширения однотипной test matrix, документации и механических исправлений. До этой точки переключаться нельзя.

## Ограничения

- IPK не собирать.
- На роутеры не устанавливать.
- `--force-reinstall` не использовать.
- UI и новые функции не добавлять до backend acceptance.
- Не отмечать фазу завершенной только по факту компиляции; требуется прохождение ее failpoint/recovery tests.
