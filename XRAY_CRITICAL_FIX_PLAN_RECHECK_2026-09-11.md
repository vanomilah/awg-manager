# Повторная проверка Critical Fix Implementation Plan

Дата: 2026-09-11  
Файл: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
SHA-256: `9A0A3AEF74B33B432F5201857CBECBF7535EDF7FEB599ADA5E10C3182C3FB9F4`  
Размер: 5833 байта  
Время изменения: `2026-09-11 09:03:36.835`

## Результат

**План не доработан и не одобрен для реализации.** Проверенная версия содержит тот же текст и те же недостатки, которые перечислены в `XRAY_CRITICAL_FIX_PLAN_REVIEW_2026-09-11.md`.

## Признаки того, что рецензия не учтена

1. В плане всё ещё предлагается новый `exec.Command` и бинарник `xray` из `PATH`, хотя нужно использовать существующие `Service.BinPath()`, resolver и `xraybin.TestConfig`.
2. Всё ещё предлагаются отдельные `apply.go`/`restart.go` без описания расширения существующей transaction state machine.
3. `SecretStore` отсутствует в Proposed Changes: нет новой модели ссылок, extraction/staging, atomic commit, resolution и миграции plaintext generations.
4. Закрывается только `GetProfile`, хотя raw config также возвращают create/update/import и могут раскрывать вложенные `RawDocument`, `StreamExtra`, `RawSettings` и `Extra`.
5. Не определена семантика merge managed config и raw/base document.
6. Не разделены `head_generation` и реально запущенная `applied_generation`.
7. E2E-тест всё ещё требует установленный Xray в CI вместо injected tester/process controller.
8. `dummy secret via SecretRef` всё ещё невозможен при текущих строковых полях модели.
9. ID-тесты не включают destructive containment, sentinel outside root, symlink/reparse point, absolute/encoded variants.
10. Из P1 по-прежнему отсутствуют atomic SecretStore, fail-closed capabilities, nested `RLock`, UTF-8 mask и стабильные API errors.

## Решение

Не начинать кодирование по этой версии. Основным документом замечаний остаётся:

`E:\AWGM\awg-manager\XRAY_CRITICAL_FIX_PLAN_REVIEW_2026-09-11.md`

## Сообщение реализующему агенту

> Вы повторно предоставили прежний план без внесения замечаний из `XRAY_CRITICAL_FIX_PLAN_REVIEW_2026-09-11.md`. Полностью перепишите `implementation_plan.md` по разделам «Блокирующие замечания», «Исправленный порядок работ» и «Acceptance». Не начинайте реализацию, пока новая версия явно не опишет SecretStore integration, устранение всех raw leaks, canonical compiler, расширение существующей transaction machine, applied-generation state и injected tests без системного Xray. После сохранения укажите новый SHA-256 файла.

