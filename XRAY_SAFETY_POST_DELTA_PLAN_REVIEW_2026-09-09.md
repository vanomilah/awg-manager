# Проверка Post-Delta Final Revision

Дата: 2026-09-09  
План: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Вердикт

Предыдущие три блокера устранены правильно: dispatcher сохраняет two-phase listener switch, migration saga больше не создаёт вложенный ingress journal, а boot orchestration стал fail-fast с компенсацией.

Однако **план пока нельзя запускать без трёх обязательных поправок**, обнаруженных при проверке псевдокода против реальных lifecycle-контрактов проекта. Главная проблема: предложенная boot-компенсация вызывает конфигурационный `Stop()` и тем самым постоянно отключает сервисы.

## Обязательные поправки

### 1. Boot-компенсация не должна использовать конфигурационный `Stop()`

В текущем коде:

- `xrayserver.Service.Stop()` выставляет `s.config.Enabled = false` и сохраняет config;
- `tgwebproxy.Service.Stop()` формирует config с `Enabled = false` и применяет его транзакционно;
- только `cdndispatcher.Stop()` является чистой runtime-остановкой.

Следовательно, интерфейс:

```go
type Lifecycle interface {
    Start() error
    Stop() error
}
```

не подходит для boot compensation. Если TG или dispatcher не запустится, обратный вызов Xray/TG `Stop()` испортит сохранённое желаемое состояние и после следующей загрузки сервис уже не стартует.

Нужны раздельные операции:

```go
type BootLifecycle interface {
    StartConfigured() error       // запускает runtime, не меняет Enabled
    ShutdownRuntime(ctx context.Context) error // останавливает runtime, не меняет Enabled
    IsRunning() bool
}
```

Названия могут быть другими, но семантика обязательна. У Xray уже есть `Shutdown(ctx)`, однако обычный `Start()` тоже записывает `Enabled=true`; желательно добавить симметричный runtime-only start. Для Telegram нужны runtime-only start/shutdown методы. Dispatcher можно адаптировать поверх существующих `Start/Stop`, поскольку его `Stop` config не меняет.

Также helper должен фиксировать состояние до запуска и компенсировать **только компоненты, которые он действительно перевёл из stopped в running**. Уже работавший до helper сервис нельзя останавливать при ошибке следующего компонента.

Обязательные тесты:

- после TG start failure Xray runtime остановлен, но `Xray.Config.Enabled` остаётся `true`;
- после dispatcher failure Xray/TG config остаются enabled;
- уже запущенный Xray не останавливается при последующей ошибке TG;
- ошибки runtime shutdown объединяются с исходной ошибкой.

### 2. Aggregated boot gate должен учитывать recovery Xray

В плане проверяются coordinator и `tgwebproxy.RecoveryRequired`, но у `xrayserver.Service` есть независимый recovery-state:

```go
func (s *Service) IsRecoveryRequired() bool
func (s *Service) RecoveryInfo() (required bool, reason string, fingerprint string)
```

Более того, существующий `xrayserver.Service.Start()` сам не проверяет `recoveryRequired`: он меняет `Enabled`, сохраняет config и вызывает restart. Поэтому пропуск Xray gate нарушает fail-closed модель.

`StartIngressIfSafe` должен получить агрегированное состояние всех трёх источников:

- coordinator recovery;
- Xray local recovery + reason;
- Telegram local recovery + reason.

При любом активном recovery ни один ingress-компонент и dispatcher не запускаются. Отдельно рекомендуется добавить защиту непосредственно в runtime/config mutation methods Xray, а не полагаться исключительно на wiring.

### 3. Saga обязана финализировать или откатывать component snapshots

Single-journal saga хранит `ComponentTransactionIDs`, но план не описывает terminal lifecycle этих snapshot.

После успешных шагов должны выполняться:

1. runtime decision записан и проверен;
2. saga phase записана как `decision_committed`;
3. для каждого committed component вызывается `FinalizePrepared(componentTxID)`;
4. каждая ошибка finalize сохраняется в saga journal и переводит coordinator в recovery-required;
5. только после успешного finalize записывается `finalized` и архивируется saga journal.

При rollback вызывается `RollbackPrepared(componentTxID)`. Если rollback не завершён, component snapshot и активный saga journal сохраняются.

Startup recovery для `decision_committed` должен повторять идемпотентный finalize, а не только доверять decision. Crash-тесты нужны до/после каждого component finalize и до archive saga journal.

## Дополнительные уточнения

### 4. Saga должна явно координировать dispatcher

Отказ от `applyLocked()` убирает не только второй journal, но и управление dispatcher. При выключении managed Xray необходимо вычислить требуемое состояние dispatcher с учётом Telegram ingress:

- если Telegram включён, dispatcher может остаться нужен;
- если ни managed Xray, ни Telegram его не используют, он должен быть остановлен либо переведён в корректное legacy-состояние;
- rollback обязан вернуть предыдущий config и running-state dispatcher.

Эти данные должны входить в saga snapshot/journal. Нельзя ограничиться только Xray component transaction ID.

### 5. Listener switch test должен ждать завершения drain

Закрытие старого listener выполняется асинхронно. Тест должен использовать bounded polling/event synchronization, а не мгновенную проверку и не фиксированный sleep, иначе получится flaky test.

### 6. Lock helper должен гарантировать unlock errors policy

Единый порядок `c.mu -> file lock` устраняет inversion. Следует вынести общий wrapper и явно обрабатывать ошибку `c.lock.Unlock()`: как минимум логировать и переводить coordinator в recovery-required, поскольку оставшийся lock способен заблокировать дальнейшие операции.

## Что теперь подтверждено

- full-state Telegram managed contract корректен;
- `ListenPort <= 0` отклоняется однозначно;
- Xray rollback сохраняет snapshot при ошибке;
- coordinator возвращает rollback errors без двойного join;
- dispatcher full replace использует общий безопасный listener-switch;
- saga использует один верхнеуровневый journal;
- lock order унифицирован;
- phase persistence стала copy-on-write;
- legacy probe учитывает daemon PID/identity;
- runtime decision failure имеет явную компенсацию;
- checksum validation recovery предусмотрена.

## Итог агенту

Архитектура уже финальная. Перед реализацией требуется:

1. заменить boot `Start/Stop` на runtime-only lifecycle без изменения `Enabled`;
2. добавить Xray local recovery в общий boot gate;
3. определить полный lifecycle component snapshots в saga;
4. включить dispatcher state в saga rollback/recovery.

После этих изменений план можно окончательно одобрить. Новый общий пересмотр архитектуры не нужен.

IPK и деплой не требуются.
