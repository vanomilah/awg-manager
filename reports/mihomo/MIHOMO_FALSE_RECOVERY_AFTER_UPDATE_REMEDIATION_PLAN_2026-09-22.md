# Mihomo: устранение ложного Recovery Mode после обновления IPK

**Дата:** 2026-09-22  
**Репозиторий:** `E:\AWGM\awg-manager`  
**Ветка:** `feature/mihomo-ai-proxyrt`  
**Статус:** готовый план реализации  
**Приоритет:** P0  
**Ограничения:** не собирать IPK и не выполнять деплой без отдельного запроса пользователя.

## 1. Наблюдаемая проблема

После установки новой версии AWG Manager Mihomo переходит в `RecoveryRequired`. Любая следующая штатная правка правила завершается ошибкой вида:

```text
preflight store digest mismatch: applied=<digest> actual=<digest>
```

UI предлагает либо пересобрать конфигурацию из настроек, либо откатиться к LKG. Такая защита полезна при настоящем повреждении данных, но не должна срабатывать после обычного обновления пакета, перезапуска процесса или обновления служебного статуса подписки.

## 2. Подтвержденные причины

### P0.1 Один digest используется для двух разных задач

`internal/mihomonative/store.go:370-378` вычисляет `CurrentDigest()` по полному сериализованному `state`.

В полном `state` находятся одновременно:

- желаемая конфигурация: прокси, подписки, группы, правила и providers;
- служебное состояние: `UpdatedAt`, `LastFetched`, `LastError` и другие поля, не меняющие генерируемую маршрутизацию.

`internal/mihomo/coordinator.go:2412-2421` сравнивает этот полный digest с `AppliedStoreDigest` и при любом отличии немедленно создаёт recovery marker.

Следствие: изменение времени последнего обновления подписки выглядит для координатора так же, как повреждение правил маршрутизации.

### P0.2 Служебный refresh меняет store вне координатора

`internal/api/mihomo_handler.go:1244-1249` после runtime-refresh вызывает `RecordSubscriptionRefresh` напрямую.

`internal/mihomonative/store.go:2530-2547` меняет `LastFetched`, `LastError`, `UpdatedAt` и сохраняет весь store. `ApplyCoordinator.MutateAndApply` при этом не вызывается, а `AppliedStoreDigest` не обновляется.

После первого такого refresh следующая обычная транзакция закономерно получает `preflight store digest mismatch`.

### P0.3 Миграция store выполняется до согласования с applied-record

`internal/mihomonative/store.go:62-106` в `NewStore` нормализует старые данные и сразу вызывает `saveLocked()`.

Это меняет байты и digest хранилища ещё до того, как координатор смог:

- определить старую версию схемы;
- проверить, что преобразование является доверенной детерминированной миграцией;
- пересобрать и проверить candidate config;
- атомарно обновить generation/LKG/applied-record.

Обычное обновление приложения поэтому может быть ошибочно классифицировано как внешняя порча данных.

## 3. Требуемая архитектура

### 3.1 Разделить три понятия digest

Нельзя ослаблять проверку до digest, который просто игнорирует всё подряд. Требуются три явных значения:

1. **DesiredConfigDigest** — канонический digest только данных, влияющих на компиляцию Mihomo.
2. **StoreSnapshotDigest** — digest точных байтов полного snapshot для проверки архива, rollback и обнаружения повреждения файла.
3. **AppliedConfigDigest** — digest реально применённого `config.yaml`.

Добавить в `NativeStoreTx` отдельные методы, например:

```go
CurrentDesiredDigest() (string, error)
CurrentSnapshotDigest() (string, error)
```

Текущее неоднозначное имя `CurrentDigest()` после миграционного периода удалить либо оставить только как приватный compatibility helper.

### 3.2 Каноническая проекция DesiredConfig

Создать отдельную версионируемую структуру, содержащую только компилируемые поля:

- параметры прокси и подписок, влияющие на конфиг;
- members и параметры групп;
- правила и rule providers;
- bridge/routing-поля, реально используемые компилятором.

Не включать:

- `CreatedAt`, `UpdatedAt`;
- `LastFetched`, `LastError`;
- runtime latency, traffic, health и прочую телеметрию;
- UI-only состояние.

Массивы, где порядок семантически не важен, сортировать. Карты сериализовать канонически. Порядок правил, где действует first-match-wins, обязательно сохранить.

Ввести `DesiredDigestVersion`, не смешивая его с `BridgeIdentityVersion` и версией JSON store.

### 3.3 Вынести оперативное состояние подписок

Предпочтительный вариант: хранить результаты refresh в отдельном runtime-файле, например:

```text
mihomo/subscription-runtime.json
```

Этот файл не входит в desired transaction, generation snapshot и applied desired digest. Его сбой не должен блокировать изменение маршрутизации.

Минимально допустимый вариант: оставить поля в основном JSON, но исключить их из `DesiredConfigDigest` и покрыть это обязательными регрессионными тестами. При этом exact snapshot digest всё равно должен проверять архивные файлы.

### 3.4 Транзакционная миграция при обновлении

`NewStore` не должен молча перезаписывать persisted store. Загрузка должна возвращать:

- исходную версию схемы;
- нормализованное представление;
- признак `MigrationRequired`;
- описание выполненных детерминированных преобразований.

Миграцией должен владеть coordinator/bootstrap reconciler:

1. Взять межпроцессный transaction lock.
2. Прочитать applied record, active config и generation snapshot.
3. Проверить старые exact digests.
4. Применить только зарегистрированную миграцию `N -> N+1` в памяти.
5. Скомпилировать candidate config.
6. Выполнить штатные validation/probe проверки.
7. Создать новую generation и LKG/applied bridge атомарно.
8. Только после commit заменить store и active config.
9. При сбое вернуть старые файлы; Recovery Mode включать только если безопасный rollback невозможен.

Миграция должна быть идемпотентной: повторный запуск после crash продолжает или откатывает ту же транзакцию, а не создаёт новый конфликт.

### 3.5 Совместимость с уже установленными legacy applied-record

Добавить в applied record:

```json
{
  "store_schema_version": 4,
  "desired_digest_version": 1,
  "applied_desired_digest": "...",
  "store_snapshot_digest": "..."
}
```

Для старой записи без этих полей разрешён только контролируемый one-time upgrade:

1. Проверить старый `AppliedStoreDigest` по snapshot соответствующей generation, а не подменять его текущим значением.
2. Нормализовать archived snapshot и текущий store одной и той же версией мигратора.
3. Сравнить их канонические desired-проекции.
4. Если desired-проекции равны и active config соответствует записи — автоматически выпустить новую согласованную generation без пользовательского Recovery Mode.
5. Если отличаются компилируемые данные либо не сходится active config — fail closed и оставить evidence.

Запрещено «лечить» mismatch простой перезаписью applied digest текущим digest: это уничтожит смысл защиты.

## 4. Изменение preflight

В `MutateAndApply` preflight должен проверять:

- `AppliedDesiredDigest == CurrentDesiredDigest()`;
- `AppliedConfigDigest == digest(active config)`;
- согласованность applied record с generation manifest/LKG pointer;
- exact snapshot digest только для неизменяемого generation snapshot.

Изменение исключительно runtime-метаданных подписки не должно блокировать новую транзакцию.

Настоящее внешнее изменение прокси, группы, правила, provider или bridge остаётся поводом для `RecoveryRequired`.

## 5. Классификация и UI Recovery Mode

Recovery marker должен содержать machine-readable reason code:

- `desired_config_mismatch`;
- `active_config_mismatch`;
- `generation_bridge_mismatch`;
- `migration_failed`;
- `snapshot_corrupted`;
- `transaction_recovery_failed`.

UI обязан показывать конкретную причину. Для штатной совместимой миграции banner вообще не показывается. Для реальной ошибки:

- **Пересобрать из настроек** — сохранить текущие пользовательские данные и создать новую проверенную generation;
- **Откатить к LKG** — восстановить предыдущий exact store snapshot и config;
- скачать evidence — без секретов.

## 6. Обязательные тесты

### Unit / integration

1. Успешный `RecordSubscriptionRefresh` меняет runtime-статус, но не `DesiredConfigDigest`.
2. Ошибка refresh меняет `LastError`, но следующая правка правила успешно применяется.
3. Изменение правила меняет `DesiredConfigDigest`.
4. Изменение порядка first-match правил меняет digest.
5. Перезапуск на той же версии не создаёт recovery marker.
6. Upgrade legacy store/applied-record выполняет one-time migration без Recovery Mode.
7. Второй перезапуск после migration ничего не меняет.
8. Crash в каждой точке migration корректно roll-forward/rollback.
9. Реальная внешняя правка desired store по-прежнему приводит к `RecoveryRequired`.
10. Повреждение generation snapshot определяется exact digest-проверкой.
11. Изменение active config определяется независимо от store.
12. Старый applied record нельзя автоматически принять, если канонические desired-проекции различаются.

### Router acceptance

На копии реальных данных:

1. Установить предыдущую версию обычным `opkg install`, без `--force-reinstall`.
2. Создать группы, правила и подписку, применить конфигурацию.
3. Обновить подписку и убедиться, что новая правка правила проходит.
4. Установить новую версию обычным `opkg install`.
5. Проверить отсутствие Recovery Mode после старта.
6. Проверить сохранность групп, правил, выбранных members и bridge allocations.
7. Повторно установить следующую сборку и повторить проверку.
8. Отдельно вне приложения изменить desired store и подтвердить, что защита действительно срабатывает.

## 7. Работа с текущим роутером до исправления

Если текущие группы и правила на экране верны, а интернет через Mihomo работает, для показанного mismatch использовать **«Пересобрать конфигурацию»**. Эта операция должна заново скомпилировать конфиг из текущего desired store и выпустить согласованную generation.

`«Откатить к LKG»` использовать только если текущие настройки ошибочны либо пересборка не проходит: откат может вернуть более старый набор правил.

После пересборки обязательно проверить:

- исчез ли Recovery Mode;
- создаётся ли новое правило;
- не возвращается ли mismatch после ручного refresh подписки и перезапуска AWG Manager.

Если возвращается после refresh — это прямое подтверждение P0.2; повторные ручные восстановления не считать приемлемым решением.

## 8. Критерии закрытия

Gate закрывается только когда одновременно выполнено следующее:

- штатное обновление IPK и перезапуск не требуют ручной пересборки;
- runtime refresh подписки не инвалидирует applied desired state;
- миграции store выполняются транзакционно и идемпотентно;
- настоящая внешняя модификация desired-конфигурации по-прежнему обнаруживается;
- все перечисленные тесты проходят в Linux/WSL;
- проведён минимум один upgrade-тест на роутере без `--force-reinstall`;
- в отчёте отдельно указаны source tests, IPK build и live router acceptance; одно нельзя выдавать за другое.

