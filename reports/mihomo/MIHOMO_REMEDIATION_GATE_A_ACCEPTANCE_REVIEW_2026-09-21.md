# Приёмочное ревью Mihomo Remediation Gate A

Проверен отчёт:

`reports/mihomo/MIHOMO_REMEDIATION_GATE_A_RESOLUTION_REPORT_2026-09-21.md`

Дата проверки: 2026-09-21.

## Вердикт

**Gate A принят. Gate B разрешено начинать.**

Основные заявления отчёта подтверждены кодом и повторным запуском тестов. Блокирующих дефектов Gate A не обнаружено.

Отчёт исполнителя требует двух редакционных уточнений, но они не блокируют переход к Gate B:

1. Нельзя утверждать, что `wdtt` и `qwdtt` сейчас чистые: оба вложенных репозитория уже содержат локальные изменения. Их gitlink SHA совпадает с зафиксированным в родительском репозитории, а проверенные timestamps изменений предшествуют Gate A, но без сохранённого baseline невозможно доказать, что исполнитель Gate A не изменил ни одного файла.
2. Формулировку «0 race conditions» следует заменить на «race detector не выявил гонок в обязательном наборе проверенных пакетов». Это не доказательство отсутствия гонок во всём репозитории.

## Что подтверждено

### 1. Ambient PATH больше не влияет на выбор verifier

В `internal/mihomo/coordinator.go` конструктор использует детерминированную схему:

```go
verifier := cfg.Verifier
if verifier == nil {
    verifier = DefaultProcessVerifier
}
```

В конструкторе отсутствуют `LookPath` и выбор `NoopProcessVerifier` через `Operator.Binary()`.

Все 15 найденных в `internal/mihomo/*_test.go` инициализаций `CoordinatorConfig` содержат явный `Verifier`; mock/fake-сценарии используют `NoopProcessVerifier`.

### 2. `dev/null` удалён из индекса

Подтверждено staged-удаление:

```text
D  dev/null
dev/null | Bin 25156817 -> 0 bytes
```

Файл размером 25 156 817 байт больше не останется в следующем состоянии репозитория после фиксации изменений.

### 3. Проверка whitespace

Повторно выполнено:

```bash
git diff --check
```

Результат: exit code 0. Git вывел только предупреждения о будущей нормализации CRLF/LF; whitespace errors отсутствуют.

### 4. Clean PATH и ambient PATH

Повторно выполнено:

```bash
PATH=/usr/local/go/bin:/usr/bin:/bin go test -count=1 ./internal/mihomo
```

Результат:

```text
ok github.com/hoaxisr/awg-manager/internal/mihomo 31.437s
```

Повторно выполнено:

```bash
PATH=/home/ivan/.local/bin:/usr/local/go/bin:/usr/bin:/bin go test -count=1 ./internal/mihomo
```

Результат:

```text
ok github.com/hoaxisr/awg-manager/internal/mihomo 31.913s
```

### 5. Race detector

Повторно выполнено:

```bash
go test -count=1 -race ./internal/mihomo
```

Результат:

```text
ok github.com/hoaxisr/awg-manager/internal/mihomo 34.333s
```

Дополнительно выполнен обязательный набор, отсутствовавший в исходном resolution report:

```bash
go test -count=1 -race ./internal/mihomonative ./internal/singbox/router ./internal/api
```

Результат:

```text
ok github.com/hoaxisr/awg-manager/internal/mihomonative 1.140s
ok github.com/hoaxisr/awg-manager/internal/singbox/router 10.318s
ok github.com/hoaxisr/awg-manager/internal/api 4.576s
```

### 6. Зависимые backend-пакеты и cmd

Повторно выполнено:

```bash
go test -count=1 ./internal/sys/procnet/... ./internal/serverwizard/... ./internal/serveringress/... ./cmd/awg-manager
```

Результат: все перечисленные пакеты `ok`.

### 7. Frontend type check

Дополнительно выполнено:

```bash
cd frontend
npm run check
```

Результат:

```text
svelte-check found 0 errors and 117 warnings in 24 files
```

Warnings не являются падением Gate A и относятся к существующим unused CSS, accessibility и Svelte diagnostics. Их нельзя записывать как «0 frontend diagnostics», но type-check прошёл.

## Состояние `wdtt` и `qwdtt`

Gitlink SHA родительского репозитория:

```text
qwdtt 7121c5b265e31b826b7b826c5bc83caa8e4d843b
wdtt  ef697994bc7ac650b81623484933d930c5cd766b
```

HEAD обоих вложенных репозиториев совпадает с этими SHA, то есть переключения gitlink/commit не произошло. Однако `git status` показывает:

```text
m qwdtt
m wdtt
```

В обоих каталогах имеются незакоммиченные пользовательские изменения. Их необходимо сохранить и не включать в работу следующих Gates. Формулировка resolution report «сабмодули не модифицировались» принимается только в значении «Gate A не должен был их менять», а не как описание текущего чистого состояния.

## Условия перехода к Gate B

1. Выполнять только Gate B, не начинать Gate C параллельно.
2. Следовать recovery-последовательности из `MIHOMO_REMEDIATION_PLAN_V5_FINAL_REVIEW_2026-09-21.md`.
3. До начала зафиксировать текстовый baseline `git status --short`, поскольку рабочее дерево содержит много прежних изменений.
4. Не изменять `wdtt` и `qwdtt`.
5. После Gate B предоставить:
   - точный список изменённых Gate B файлов;
   - diff только Gate B;
   - failpoint matrix с полным выводом тестов;
   - повторный обязательный race-набор;
   - `git diff --check`;
   - подтверждение отсутствия IPK/deploy.

## Итог

Gate A выполнен достаточно для перехода дальше. Следующее ревью должно принимать реализацию Gate B по фактическим crash/recovery-инвариантам, а не по формулировке отчёта исполнителя.
