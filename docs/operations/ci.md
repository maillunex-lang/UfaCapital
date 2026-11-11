# GitHub Actions: требуемые реальные проверки

Этот документ — спецификация. Workflow-файлы сейчас не создаются: до разрешения нет кода/модулей/lockfile, следовательно нечего честно собирать. Каждый workflow появляется вместе с соответствующей реализацией, а не как фиктивная зелёная проверка.

| Workflow/этап | Когда добавить | Что обязан реально выполнить | Доказательство |
|---|---|---|---|
| docs-check | F00 | локальные ссылки, schema карточек/status/dependencies, no-secrets базовая проверка | отчёт/exit !=0 при ошибке |
| server-check | F01, расширение F03–F06 | gofmt diff, go vet ./..., go test ./..., race для concurrency; критические fuzz/property | test report/log/version |
| db-migrations | F01 | миграция пустой PG, upgrade с предшествующей схемы, schema constraints | реальная PG service, не mock |
| api-integration | F02–F06 | auth/BOLA/idempotency/рынок/рецепты/агенты на server+PG | assertions данных и балансов |
| godot-check | F02 | gdlint/gdformat check при совместимости, headless import, parser, собственные integration scenes | реальные exit codes, ошибки fail |
| admin-check | F01, рост по механикам | npm ci, typecheck, tests, production build, Playwright RBAC/form flows | build + browser evidence |
| android-build | F02, итог F10 | точные Godot/templates/JDK/SDK; exportAPK, apksigner verify, manifest/package/version, APK hash/size | настоящий APK artifact |
| android-device | F10/F13 | install/run/login/trade/reconnect на emulator и выбранном физическом среднем смартфоне | device/OS/run logs, manual evidence |
| economy-simulation | F06/F08 | fixed seed, 7/30 дней, money/inventory invariants, AI behavior/shocks | параметры, результаты, failed scenarios |
| release | F13 | подписанная release APK, upgrade, protected smoke; AAB отдельно при магазине | checksum/cert fingerprint, run evidence |

PR jobs запускаются на изменении своей области с путями и зависимостями; защищённый release pipeline проверяет все обязательные компоненты текущего milestone. Path filtering не даёт право считать компонент проверенным после изменения его зависимости. Не использовать continue-on-error, `|| true`, `echo success` или пропуск отсутствующего SDK как доказательство успеха.

Без секретов на fork PR. GITHUB_TOKEN permissions read-only по умолчанию; upload/release отдельные минимальные права. Actions закрепляются full SHA после проверки официального repo; toolchains/images digest, npm lock, Go sums и Python lock фиксируются. Dependabot/аналог может предложить обновление, но не применяется без совместимых проверок. Package scripts читаются до установки зависимостей.

APK CI сборка не доказывает удобство/скорость устройства. Android emulator не заменяет итоговый physical smoke. Недоступный signing secret или устройство делает соответствующий gate BLOCKED; остальные честные проверки могут пройти отдельно. Keystore вне Git, отдельный staging package/signature, release signing backup защищён. Выкладка на VPS требует разрешения задачи, а не просто успешного build.
