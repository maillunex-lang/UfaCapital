# Логическая модель данных

Не DDL. Миграции создаются только после разрешения. UUID/PK, FK, ограничения и индексы фиксируются при реализации вместе с тестами fresh+upgrade.

| Группа сущностей | Основные данные и связи | Инвариант |
|---|---|---|
| users/sessions/roles | identity, password_hash, session hash/expiry, roles | сессии отзываемы, credentials не в ответах |
| vip_entitlements | user, starts/expires, source, audit | entitlement проверяется временем сервера |
| companies/owners | HUMAN/AI/TEST_ADMIN, user/agent reference, city, status | активный владелец; AI label не клиентский |
| accounts/reservations | owner/type/currency, available/reserved projections | резервы не превышают средства; исключения только у явного emission account |
| ledger_transactions/entries | operation_id, immutable postings, metadata | сумма проводок по валюте=0, уникальная операция |
| cities/goods/recipe_versions/config_versions | stable IDs, version/effective_at, inputs/outputs | удаление используемого ID запрещено, деактивация вместо удаления |
| enterprise_templates/enterprises/lines | company, city, lease, capacity, state/version | только оплаченная мощность, ограничение аренды/земли без spatial build |
| workforce/payroll | city groups, hired, wage, qualification | штат не отрицателен, зарплата обеспечена |
| warehouses/lots/inventory_moves | good, owner, quality class, age, qty, cost, reserved | qty>=reserved>=0; движение связано с операцией |
| production_jobs | recipe_version, line, tick, inputs/outputs, status | уникальный job+tick, атомарный расход и выпуск |
| orders/trades | city, good, side, price, qty, reserved, sequence | price-time порядок; остаток не отрицателен |
| contracts/deliveries | parties, goods/qty/price/deadline, reservations, route/status | однократное исполнение, нет товара одновременно в пути и свободного |
| loans/collateral | lender fund, company, amount, rate, schedule, collateral IDs | один залог не дублируется, просрочка наблюдаема |
| population_budgets/external_limits | group budgets, category allocation, import/export caps | нет бесконечных закупок или неявной эмиссии |
| agents/agent_memory/decision_log | profile, seed, estimates, actions/reasons, funds | durable memory, не отдельная экономическая БД |
| world_ticks/jobs/outbox | period, step, unique operation, event cursor, published flag | replay без повторной проводки/партии |
| assets/asset_versions/manifests | ID, prompt, provenance/license, file hash, target/runtime policy | каждый используемый графический ID в каталоге, атомарная публикация |
| admin_audit/events | actor, reason, before/after, effective interval, correlation ID | audit вместе с успешной мутацией, immutable |

## Транзакционная граница

Команда блокирует связанные счета/лоты/заявки в одном порядке по ID, повторно валидирует состояние, пишет ledger+inventory+domain change+audit/outbox и commit. Никаких внешних HTTP/LLM-вызовов внутри транзакции. Основной уровень READ COMMITTED с явными блокировками и уникальными constraints; сложные конкурирующие операции могут использовать SERIALIZABLE с ограниченным retry. SQL isolation выбирается тестом, а не предположением о гонках.

Баланс может быть materialized projection, но не независимой правдой: сверяется с ledger. Исправление ошибки — новая компенсирующая операция. Запрет отрицательных значений не распространяется механически на технический счёт эмиссии; его семантика и лимит задаются явно.

Миграции: append-only после публикации, checksum и lock запуска; каждая tested empty+previous snapshot. Добавление nullable/default → backfill с лимитом → ограничения; разрушительные изменения требуют backup и отдельной задачи. Откат приложения не обещает автоматического отката схемы. Production seed отделён от dev fixture; нельзя повторно начислить грант через повторный seed.
