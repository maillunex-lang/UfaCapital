# UFACAPITAL — главный навигатор

**A — Деловая республика:** общий рынок Башкортостана, производство и специализация компаний. Godot 4/GDScript/Android landscape; Go+PostgreSQL18, Ubuntu24.04/Compose/Nginx; React/TS/Vite admin. Истина денег/ресурсов — только сервер.

**Активно:** [GATE-CODE](roadmap/tasks/GATE-CODE.md), BLOCKED — ожидается отдельное разрешение заказчика. **Следующая:** [F00-T01](roadmap/tasks/F00-T01.md). Программирование пока не разрешено; выбор «А» утверждает концепцию.

| Подготовка | Статус | Результат |
|---|---|---|
| 1: концепции/выбор | DONE | [5 вариантов](docs/concept/README.md), DEC-001 |
| 2: инструменты | DONE | [каталог](ai/skill-catalog.md), [совместимость](ai/tool-compatibility.md); статическая проверка, не runtime сертификация |
| 3: архитектура/дорожная карта | DONE | [спецификации](docs/architecture/overview.md), [14 этапов/28 задач](roadmap/README.md), проверены ссылки/статусы/зависимости |

| Реализация | Статус | Зависимость |
|---|---|---|
| [F00 среда/контракты](roadmap/phases/F00-foundation.md) | BLOCKED | GATE-CODE |
| [F01 сервер/БД/auth/admin](roadmap/phases/F01-server-identity-admin.md) | NOT_STARTED | F00 |
| [F02 Android/VIP/test-admin](roadmap/phases/F02-android-access.md) | NOT_STARTED | F01 |
| [F03 ledger/компании/кредит](roadmap/phases/F03-ledger-companies.md) | NOT_STARTED | F02 |
| [F04 склад/работники/выпуск](roadmap/phases/F04-production-inventory.md) | NOT_STARTED | F03 |
| [F05 рынок/доставка/контракты](roadmap/phases/F05-trade-contracts.md) | NOT_STARTED | F04 |
| [F06 ИИ/симуляция](roadmap/phases/F06-ai-simulation.md) | NOT_STARTED | F05 |
| [F07 ассеты/полнотаadmin](roadmap/phases/F07-assets-admin-complete.md) | NOT_STARTED | F06 |
| [F08 recovery/security/load](roadmap/phases/F08-recovery-security.md) | NOT_STARTED | F07 |
| [F09 stagingVPS/backup](roadmap/phases/F09-vps-operations.md) | NOT_STARTED | F08 |
| [F10 первый срез](roadmap/phases/F10-vertical-acceptance.md) | NOT_STARTED | F09 |
| [F11 региональное развитие](roadmap/phases/F11-regional-growth.md) | NOT_STARTED | F10 |
| [F12 объединения/инвестиции](roadmap/phases/F12-cooperation-investment.md) | NOT_STARTED | F11 |
| [F13 проверка/выпуск](roadmap/phases/F13-release.md) | NOT_STARTED | F12 |

Критерий срезаF10: установленныйAPK→HTTPSVPS→реальный обмен 2 людей+2 ИИ→офлайн/рестарт→admin/VIP/grants/assets→автотесты/restore. [Полная приёмка](docs/gameplay/vertical-slice.md). Нет обязательного доказательства — нет DONE.

Порядок работы: [AGENTS](AGENTS.md) → [PROJECT_CONTEXT](PROJECT_CONTEXT.md) → одна карточка → минимальные ссылки/реализация. Статусы: NOT_STARTED, IN_PROGRESS, BLOCKED, REVIEW, DONE; определения в [индексе](roadmap/README.md).

Ключевые документы: [API](docs/architecture/api.md), [данные](docs/architecture/data-model.md), [экономика](docs/economy/core.md), [рынок](docs/economy/market.md), [ИИ](docs/ai-players/agents.md), [Android](docs/ui/android.md), [admin](docs/ui/admin.md), [CI](docs/operations/ci.md), [VPS](docs/operations/deployment.md), [тесты](docs/testing/acceptance.md), [решения](docs/decisions/DECISIONS.md), [передача](roadmap/WORKLOG.md).

[Отчёт о подготовке и фактических проверках](docs/preparation-report.md).
