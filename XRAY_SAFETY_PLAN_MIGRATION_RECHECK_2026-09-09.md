# Проверка финального плана миграции Xray

Дата: 2026-09-09  
Проверен файл:
`C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## Итог

План почти готов к реализации. Архитектура legacy migration, rollback, recovery state,
единственного владельца `cmd.Wait()` и fail-closed uninstall проработана правильно.
Перед началом реализации необходимо устранить четыре несоответствия.

## Обязательные поправки

### 1. Нельзя назначать один legacy-порт двум компонентам

В текущем плане указано:

```text
inbounds[0].port -> ListenPort, DispatcherPort
```

Это может назначить Xray runtime и CDN dispatcher один порт и привести к конфликту
listener. Правильная миграция:

- `legacy inbounds[0].port` переносится только в `ListenPort` Xray-сервера;
- `DispatcherPort` читается из существующей конфигурации dispatcher, если источник
  однозначно определён;
- если значение определить нельзя, allocator выбирает отдельный свободный управляемый
  порт;
- preflight подтверждает, что `ListenPort != DispatcherPort` и оба порта доступны либо
  заняты именно мигрируемыми процессами;
- выбранные значения включаются в preview и migration report.

Запрещено молча копировать один порт в оба поля.

### 2. Конструктор `xrayserver.New()` не выполняет мутации

`New()` не должен останавливать процессы, создавать backups, менять init script или
запускать миграцию. Конструктор выполняет только безопасную загрузку собственного state.

Рекомендуемый lifecycle:

```text
xrayserver.New()
  → read-only LegacyDiscovery
  → публикация discovered state
  → wiring запускает отдельный MigrationRunner
  → preflight и conflict decision
  → подтверждённая transaction
```

Даже автоматическая однозначная миграция выполняется явным runner после полной
инициализации зависимостей, locks, process registry и event/logging infrastructure.
Повторный вызов конструктора в тесте или вспомогательном коде не должен изменять систему.

### 3. `migration_conflict` отображается в серверном разделе

Разрешение конфликта legacy/new config относится к настройке Xray-сервера и должно
находиться в:

```text
Серверы → Xray
```

`settings/+page.svelte` и карточка системной интеграции не должны содержать выбор
конфигурации, процессы, порты или migration wizard. В интеграции остаются только:

- бинарник установлен/отсутствует;
- версия или `Версия не определена`;
- `Скачать`/`Удалить`.

Frontend conflict UI следует реализовать в `XrayServerCard.svelte` либо отдельном
компоненте Xray server recovery/migration wizard.

### 4. Безопасная работа с legacy init script

Нельзя безусловно выполнять `chmod 0644` по пути
`/opt/etc/init.d/S99xray-cdn`: путь может быть symlink, иметь нестандартный режим или
конфликтующее disabled-имя.

До изменения необходимо:

- вызвать `os.Lstat`, не следуя symlink автоматически;
- определить regular file/symlink и проверить resolved target в разрешённом каталоге;
- сохранить исходное имя, тип, link target, mode и fingerprint;
- проверить отсутствие коллизии целевого `.disabled` имени;
- применять переименование/изменение режима атомарно насколько позволяет файловая
  система;
- непосредственно перед изменением повторно сверить fingerprint;
- при rollback точно восстановить исходное имя, symlink target и permissions.

Если тип или target нельзя безопасно подтвердить, миграция работает fail-closed и
переходит в ручное разрешение конфликта.

## Дополнение: обнаружение legacy-процесса

Legacy PID-файл может отсутствовать. Использование `pidof xray` запрещено. Discovery
может выполнить ограниченный просмотр числовых каталогов `/proc` и выбрать только процесс,
для которого одновременно подтверждены:

- полный executable identity;
- cmdline с точным нормализованным путём `/opt/etc/xray-cdn/config.json`;
- process start time;
- ожидаемый listener;
- отсутствие неоднозначности между несколькими кандидатами.

Если найдено несколько подходящих процессов либо `/proc` нельзя надёжно прочитать,
автоматическая остановка запрещена и возвращается `migration_conflict`.

## Критерий допуска

План можно запускать после внесения следующих изменений:

1. раздельное определение `ListenPort` и `DispatcherPort`;
2. перенос мутаций из `xrayserver.New()` в явный `MigrationRunner`;
3. перенос migration conflict UI из Settings в `Серверы → Xray`;
4. symlink-safe и rollback-safe управление legacy init script;
5. точное обнаружение legacy-процесса без `pidof`.

После реализации проверить это отдельными тестами конструктора без side effects,
port allocation/conflict, init symlink collision, ambiguous `/proc` discovery и полного
rollback исходного init state.
