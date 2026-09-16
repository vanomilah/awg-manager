# Closure recheck: Xray / Server Ingress Safety

Дата: 2026-09-10  
Проверенный документ: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`

## Вердикт

Замечание P0 из `XRAY_SAFETY_WALKTHROUGH_FINAL_ACCEPTANCE_RECHECK_2026-09-10.md` исправлено корректно. Неоднозначность «socket отсутствует / socket найден, но PID неизвестен» устранена, ошибки чтения обязательных socket tables закрывают миграцию, IPv4/IPv6 рассматриваются раздельно, негативные тесты добавлены.

Архитектурных и P0-блокеров больше не найдено. Для окончательного закрытия остаётся **одна точечная правка P1** к идентификации executable.

## Подтверждённое исправление прошлого P0

Введён результат `listenerLookup` с отдельными полями `SocketFound`, `SocketInode` и `PID`. `findListeningProcess` возвращает также ошибку чтения procfs.

Теперь:

- отсутствующий socket в успешно прочитанной релевантной таблице отличается от socket с неизвестным PID;
- socket с неизвестным PID даёт `LegacyProbeConflict`;
- ошибка чтения таблицы даёт `LegacyProbeConflict`;
- для IPv6 обязательна `net/tcp6`, для IPv4 обязательна `net/tcp`;
- открытый TCP-порт без подтверждённого procfs socket/PID также даёт конфликт.

Тесты `TestMigrationSaga_ProcfsListeningSocketUnmappedPIDFailsClosed` и `TestMigrationSaga_SocketTableErrorsAndAddressFamilies` соответствуют заявленным сценариям.

## Оставшееся замечание

### P1 — имя executable проверяется подстрокой, а не точным допустимым именем

Файл: `internal/serveringress/migration_saga.go:607-628` в текущей редакции.

Код использует:

```go
if !strings.Contains(cmdline, "xray") { ... }
...
if !strings.Contains(exeBase, "xray") { ... }
```

Из-за этого executable с basename `notxray`, `xray-wrapper` или `fake-xray-daemon` будет принят как Xray. Проверка всей cmdline не компенсирует проблему: ожидаемый аргумент конфигурации `/opt/etc/xray-cdn/config.json` сам содержит строку `xray`, поэтому условие cmdline выполняется даже для чужого argv[0].

Проектный resolver ищет реальные бинарники `/opt/bin/xray`, `/opt/sbin/xray` и принимает basename `xray` (для иных платформ также `xray.exe`). На целевой Linux/Entware-платформе проверка legacy-процесса должна требовать как минимум:

```go
exeBase := strings.ToLower(filepath.Base(exe))
if exeBase != "xray" {
    return LegacyProbeConflict, ...
}
```

Лучше вынести допустимое имя в общий helper с `internal/xrayserver/xraybin`, чтобы resolver и migration probe не расходились. Проверку `strings.Contains(cmdline, "xray")` следует удалить или заменить проверкой `argv[0]`; главным доказательством executable остаётся `/proc/<pid>/exe`.

Обязательные тесты:

- `/opt/bin/notxray` — conflict;
- `/opt/bin/xray-wrapper` — conflict;
- `/opt/bin/xray` — running;
- cmdline чужого процесса с аргументом `/opt/etc/xray-cdn/config.json` и executable `/opt/bin/other` — conflict.

После этой правки walkthrough можно принять окончательно без дополнительного архитектурного этапа.

## Повторная проверка

### Go race detector

```text
go test -count=1 -race ./internal/tgwebproxy/... ./internal/xrayserver/... ./internal/cdndispatcher/... ./internal/serveringress/... ./internal/api/... ./cmd/awg-manager
```

Результат: exit code 0, все пакеты прошли.

### Linux ARM64 build

```text
GOOS=linux GOARCH=arm64 go build -o /tmp/awg-manager-audit3 ./cmd/awg-manager
```

Результат: exit code 0.

### Frontend

```text
npm exec -- svelte-check --threshold error
```

Результат: 0 ошибок, 118 предупреждений.

### Git diff check

`git diff --check`: exit code 0. Имеются только информационные предупреждения CRLF/LF.

IPK не собирался. Роутеры не изменялись.
