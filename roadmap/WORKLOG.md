# Передача контекста и границы работы

Обновлено 11.11.2026. DEC-002 разрешил программирование F00–F10. GATE-CODE снят. Владелец текущей задачи — Grok, ветка main, base `7f12d29543373ac9aafa0cea196bb75f3abc176b`.

| Задача | Владелец | Ветка/base | Область | Статус |
|---|---|---|---|---|
| F00-T01 | Grok | main / 7f12d29 | toolchain, compose, .env.example, .gitignore | BLOCKED: нет docker; SDK и debug APK есть |
| F01 сервер | Grok | main / 7f12d29 | server/, tests, web/desk, client/ | IN_PROGRESS |

Сделано: миграции, ledger, производство grain→flour→bread, рынок, контракт, кредит, два ИИ, идемпотентность, интеграционный тест на PostgreSQL 18.6, стол браузера, клиент Godot 4.7.2 и отладочный APK. Не сделано: установка APK на телефон, VPS/HTTPS, админка React, замена ассетов, человеческая приёмка. Следующий шаг: отдать APK на скачивание; F00 остаётся BLOCKED без `docker compose config`. Не переходить к F11.

Шаблон назначения: taskID, владелец/инструмент, branch/worktree, baseSHA, allowlist файлов, API/миграции, начало, зависимость. Перед правкой проверить свежий git status/log/origin. Одна область — один владелец; для конфликтующей задачи отдельный PR, не перезапись main.

Шаблон handoff: выполнено; ещё не выполнено; реальные команды/результаты; blocker; изменённый контракт; следующий маленький шаг; ссылки на карточку/evidence. Большие логи не вставлять сюда. По завершении сохранять краткую историю в карточке, текущую таблицу не превращать в журнал всех сессий.

Ограничения последней проверки среды: Godot 4.6.3, Java21, Docker CLI присутствуют; `/usr/bin/go version` не подтвердил Go SDK; adb/sdkmanager не найдены; VPS и Android-устройство не предоставлены. Это наблюдения этапа 2, будущая сессия должна проверить свою среду заново.

Опубликовано: код среза `afab29ec7f89bf0e6b4fbfab560016f46cfaf26a`, правка тестов `a307463884c5e278fb5310f9f04f3fc27c46a247` на origin/main. GitHub Actions [server/43076137080](https://github.com/maillunex-lang/UfaCapital/actions/runs/43076137080) на `a307463` завершился успешно (`go test`, `docs-check`). Предыдущие два прогона падали: `DATABASE_URL` подменял имя роли, а не имя базы.

11.11.2026, APK: `sdkmanager --list_installed` показал platform-tools 37.0.1, build-tools 36.1.0, android-35/36, NDK 29.0.14206865. Godot 4.7.2 `--export-debug Android` завершился с кодом 0. `aapt dump badging`: `dev.ufacapital.game`, versionName 0.1.0, minSdk 29, targetSdk 36, landscape, INTERNET, cleartext. `apksigner verify` — v2. SHA-256 APK `b138267bc530f7d5171f8c8a6cdea08936f81008a22b414d5eb448f567ca1157`, 166312212 байт. Файл и keystore вне Git. `adb devices` пуст. Первый Gradle с `-Xmx4536m` убит OOM; повтор с `-Xmx1024m` прошёл.
