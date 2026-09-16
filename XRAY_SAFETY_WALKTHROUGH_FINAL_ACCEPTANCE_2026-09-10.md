# Финальная приёмка Xray / Server Ingress Safety walkthrough

Дата: 2026-09-10  
Проверенный документ: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`

## Вердикт

**Принято.** Открытых P0/P1 замечаний по заявленному safety scope не осталось. Walkthrough соответствует текущей реализации и тестам.

## Последняя закрытая правка

- Добавлен общий `xraybin.IsXrayExecutableName` с точным допустимым именем.
- Legacy probe отдельно проверяет basename `argv[0]`.
- `/proc/<pid>/exe` проверяется тем же общим helper.
- Имена `notxray`, `xray-wrapper`, `fake-xray-daemon` и посторонний argv[0] отклоняются.
- Путь `/opt/etc/xray-cdn/config.json` в аргументах больше не может сам по себе выдать чужой процесс за Xray.
- Resolver и migration probe используют единое правило идентификации.

Негативные тесты для перечисленных сценариев добавлены и проходят.

## Ранее закрытые safety-пункты

- одновременное наличие active/disabled init scripts завершается конфликтом до изменений;
- tri-state legacy probe работает fail-closed;
- timeout и неопределённые dial errors не считаются остановленным сервисом;
- `ECONNREFUSED` требует успешно проверенного отсутствия socket в procfs;
- socket с неизвестным PID возвращает conflict;
- ошибки и неоднозначности IPv4/IPv6 socket tables возвращают conflict;
- legacy config обнаруживается без fallback на выдуманный `127.0.0.1:443`;
- конфиг процесса сопоставляется с точным launch argument;
- ошибка чтения executable link возвращает conflict;
- rollback сохраняет процесс, работавший до saga;
- ошибки rollback/recovery и shutdown не скрываются;
- runtime shutdown не меняет persistent `Enabled`.

## Выполненная проверка

### Go race detector

```text
go test -count=1 -race ./internal/tgwebproxy/... ./internal/xrayserver/... ./internal/cdndispatcher/... ./internal/serveringress/... ./internal/api/... ./cmd/awg-manager
```

Результат: exit code 0; все перечисленные пакеты прошли без race warnings.

### Linux ARM64 build

```text
GOOS=linux GOARCH=arm64 go build -o /tmp/awg-manager-final-accept ./cmd/awg-manager
```

Результат: exit code 0.

### Frontend

```text
npm exec -- svelte-check --threshold error
```

Результат: 0 ошибок, 118 предупреждений.

### Git diff check

`git diff --check`: exit code 0. Выведены только информационные предупреждения о будущей нормализации CRLF/LF.

IPK не собирался. Установка на роутеры не выполнялась.
