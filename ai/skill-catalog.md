# Каталог скиллов, инструкций и инструментов

Проверено 11.11.2026. Польза: 1 — низкая, 10 — критическая для проекта. «Выбран» означает решение проектирования; зависимости ещё не установлены и тесты runtime не выполнены. Сторонний код и тексты скиллов в репозиторий не перенесены.

## Внешние кандидаты

| ID, источник | Лицензия, проверка | Совместимость/назначение, польза | Безопасность/зависимости | Решение и необходимость установки |
|---|---|---|---|---|
| C01 [openai/skills](https://github.com/openai/skills) | README требует проверять LICENSE.txt каждого скилла; корневой LICENSE дал 404 | Ранее каталог Codex; README прямо помечает deprecated; 3/10 | Содержит скрипты установщика; общая лицензия не установлена | Исключён как устаревшая база; не устанавливать |
| C02 [openai/plugins](https://github.com/openai/plugins), build-web-apps/frontend-testing-debugging | SKILL.md прочитан; общего LICENSE и лицензии выбранного пакета по проверенным путям нет; условия распространения не подтверждены | Актуальный пример плагинов Codex, браузерный QA; 6/10 | Зависит от Browser plugin и его runtime, содержит правила исполнения/ограничения fallback; весь пакет не аудирован | Не копировать/не устанавливать; собственная инструкция тестирования |
| C03 [Anthropic webapp-testing](https://github.com/anthropics/skills/tree/main/skills/webapp-testing) | Индивидуальный LICENSE.txt Apache-2.0 прочитан | Claude, частично общий Agent Skills; веб-тесты, 7/10 | Python+Playwright и helper; инструкция предлагает запускать helper до чтения исходника, что противоречит проверке непроверенного кода | Не импортировать, использовать собственный безопасный маршрут. При будущем импорте аудит scripts, сохранение лицензии/NOTICE |
| C04 [Anthropic frontend-design](https://github.com/anthropics/skills/tree/main/skills/frontend-design) | Индивидуальный LICENSE.txt Apache-2.0 прочитан | Web UI, для Godot только идеи; 6/10 | Не даёт гарантий Godot/Android UX; не нужен доступ к продакшену | Исключён как дублирующий собственную инструкцию UI; без установки |
| C05 [Agent Skills standard](https://agentskills.io/specification) | Текст стандарта используется как справочник, не копируется; разрешение на перепубликацию целиком не проверялось | name/description/SKILL.md, 9/10 | Не исполняется, внешних runtime-зависимостей нет | Основа формата собственных адаптеров; установка не нужна |
| C06 [Godot 4 docs](https://docs.godotengine.org/en/stable/), [engine](https://github.com/godotengine/godot) | Engine MIT; docs CC-BY-3.0 по отдельным LICENSE.txt; не смешивать | Godot/GDScript/HTTP/Android/profiler, 10/10 | Engine и export templates одной версии; JDK/SDK, контроль загрузок | Собственные инструкции; установку/пинning выполнять в F00 после разрешения |
| C07 [gdtoolkit](https://github.com/Scony/godot-gdscript-toolkit) | MIT, LICENSE прочитан | gdlint/gdformat для GDScript, 8/10; проверка синтаксиса движком всё равно нужна | Python, зависимости/версия должны быть зафиксированы; не заменяет Godot parser | Выбран для будущего lint при smoke-проверке совместимости; сейчас не установлен |
| C08 [GUT](https://github.com/bitwes/Gut) | Проверенные raw LICENSE.md и LICENSE вернули 404; лицензия не подтверждена этим исследованием | README: 9.x для Godot4; точная совместимость с выбранным релизом не проверена, 6/10 | Сторонний плагин, исполняемый GDScript | Отложен, не копировать; начальные клиентские проверки — собственный headless runner |
| C09 [Go testing](https://go.dev/doc/tutorial/add-a-test), [Go](https://github.com/golang/go) | BSD-3-Clause, LICENSE прочитан | Backend, race/fuzz/bench, 10/10 | Официальный SDK, проверка версии/контрольной суммы; установка в F00 | Выбран, не сторонний AI-скилл |
| C10 [PostgreSQL](https://www.postgresql.org/docs/current/explicit-locking.html), [pgx](https://github.com/jackc/pgx) | PostgreSQL License; pgx MIT, проверены | Транзакции/блокировки/Go driver, 10/10 | Настоящая PostgreSQL в тестах, TLS/пулы/миграции, зависимости закрепить | Выбраны как стек, не устанавливались |
| C11 [Mesa](https://github.com/projectmesa/mesa) | Apache-2.0, LICENSE прочитан | Агентные модели, Python; не Go runtime, 6/10 | Python и научные зависимости, отдельная модель может разойтись с игрой | Не включать в runtime; собственные алгоритмические агенты и симулятор на той же Go-логике |
| C12 [React](https://github.com/facebook/react), [TypeScript](https://github.com/microsoft/TypeScript), [Vite](https://github.com/vitejs/vite) | MIT / Apache-2.0 / MIT, прочитаны | Отдельная браузерная админка, 8/10 | Node LTS, lockfile, npm lifecycle scripts/transitive licenses проверить | Выбраны для будущей админки, без установки; минимальные формы, без сложного конструктора |
| C13 [Playwright](https://playwright.dev/docs/test-assertions) | Apache-2.0 по LICENSE microsoft/playwright | Admin E2E, не заменяет Android-тест, 9/10 | Node+браузер; тестовые учётки и isolated DB | Выбран на будущий QA. Ожидать конкретное состояние UI, не бесконечный networkidle при WS |
| C14 [OWASP ASVS](https://github.com/OWASP/ASVS) | CC-BY-SA-4.0 по LICENSE.md | Требования безопасности, 9/10 | Справочник; не исполняется, внешний сканер сам по себе не доказывает безопасность | Использовать ссылки и собственные проверки, не копировать стандарт |
| C15 [Docker Compose](https://docs.docker.com/compose/), [Nginx](https://nginx.org/en/docs/), [GitHub Actions](https://docs.github.com/en/actions/reference/security/secure-use) | Документация как справочник, условия её перепубликации не проверялись | VPS, CI/CD, 10/10 | Docker host привилегирован; actions pin SHA, минимум permissions, секреты вне Git | Собственные инструкции, точные образы/зависимости и их лицензии проверить перед установкой; сейчас не развёрнуто |
| C16 [xAI tools](https://docs.x.ai/developers/tools/overview), [function calling](https://docs.x.ai/developers/tools/function-calling) | Условия API/документации сервиса, не лицензия переносимого SKILL.md; не копируем | Grok как помощник разработки, 8/10 | Ключ/оплата/API runner, запросы могут передавать код провайдеру; экономический runtime не зависит от LLM | Текстовый task packet; API runner и платные запросы не создавались |

Это проверка релевантных кандидатов, а не полный аудит всей истории и всех зависимостей проектов. Неизвестная лицензия — основание не переносить материал, а не утверждение, что лицензии нигде не существует.

## Подготовленные оригинальные инструкции

| ID/имя адаптера | Общий документ | Когда нужен | Польза | Будущие зависимости |
|---|---|---|---:|---|
| uc-task | [task-flow](shared/task-flow.md) | Ведение задачи, контекст, документация/Git | 10 | Git, доступ к задаче |
| uc-economy | [economy](shared/economy.md) | Деньги, рынок, рецепты, спрос | 10 | Go/PostgreSQL для реализации |
| uc-agents | [agents](shared/agents.md) | Алгоритмические предприниматели | 10 | Та же серверная модель |
| uc-server-db | [server-db](shared/server-db.md) | Go API, БД, миграции, сеть | 10 | Go, PostgreSQL |
| uc-godot-android | [godot-android](shared/godot-android.md) | GDScript, Android, оптимизация | 10 | Godot/templates/JDK/SDK/устройство |
| uc-admin-assets | [admin-assets](shared/admin-assets.md) | Админка, права, замена графики | 10 | React/TS/Vite, Go API |
| uc-ui | [ui](shared/ui.md) | Android UI/UX и web forms | 9 | Godot либо browser preview |
| uc-testing-security | [testing-security](shared/testing-security.md) | Инварианты, негативные тесты, QA/security | 10 | Среда из задачи, без обязательного внешнего сканера |
| uc-ops-ci | [ops-ci](shared/ops-ci.md) | CI/CD, VPS, бэкапы/мониторинг | 10 | GitHub Actions, Docker, VPS на соответствующем этапе |

Все девять текстов созданы для проекта с нуля. Нет копирования сторонних скиллов/скриптов; нет установки исполняемого кода. Лицензия всего проекта и публичного переиспользования его собственных инструкций отдельно заказчиком не выбрана; этот этап не назначает MIT/Apache всем материалам автоматически. Внутри проекта инструкции доступны трём AI-инструментам через адаптеры/пакет.

## Покрытие требований

Codex/Claude/Grok — compatibility + uc-task; Godot/GDScript/Android/performance — uc-godot-android; MMO/Go/PostgreSQL — uc-server-db; экономика — uc-economy; ИИ — uc-agents; админка/ассеты — uc-admin-assets; UI/UX — uc-ui; тестирование/безопасность — uc-testing-security; CI/CD/эксплуатация — uc-ops-ci; документирование — uc-task.
