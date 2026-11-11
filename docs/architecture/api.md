# Контракт команд и API

Это спецификация, не готовый OpenAPI. F00 фиксирует машиночитаемый OpenAPI3.1 из этого контракта до реализации экономических команд; далее контракт меняется вместе с клиентом/сервером и совместимыми тестами.

## Общий формат

UTC RFC3339, opaque UUID, деньги/количества/счётчики int64 — строки с десятичными цифрами; цена задаётся в минимальных игровых денежных единицах на единицу товара. Версии сущностей — целые неотрицательные значения в таком же формате. Ответ содержит request_id, server_time, data либо error{code,message,details}; ошибки не раскрывают чужие данные/SQL. Списки — cursor+limit (до 100).

Каждая экономическая POST-команда требует Idempotency-Key, scoped actor+route. Сохраняется hash канонического тела/результат в той же транзакции; тот же key и тело → прежний ответ, иное тело →409. Хранение результата минимум 30 дней, уникальный operation_id/доменный constraint предотвращает повтор даже после архивации кэша. Дедлок/serialization повторяются сервером ограниченно; клиент не получает частичное выполнение. If-Match/version для настроек и изменяемых заявок; конфликт требует перечитать.

## Поверхность первого среза

| API-группа | Чтение | Команды | Проверки |
|---|---|---|---|
| auth | me/session | register, login, refresh, logout | rate limit, пароль hash, rotation/revoke |
| world/catalog | cities, goods, recipes, versions, market summary | нет player write | только действующие публичные версии |
| companies | company, finances, ledger view | create, settings | 1 активная компания/аккаунт; grant once |
| enterprises | lines, workforce, state | rent/open, expand, conserve, hire | деньги, город, мощность, роли |
| inventory | lots, reserved/free, warehouse | transfer/request delivery | собственность, место/доставка |
| production | orders/history | start/stop policy | рецепт, входы, место, payroll |
| market | orderbook/trades | place/cancel/accept | цена/объём, остатки/резервы, владение |
| contracts | offers/active/history | offer/accept/cancel permitted | два согласия, объём/срок, правила обеспечения |
| loans | offers/active/schedule | borrow/repay | обеспечение, лимит, фонд кредитора |
| agents | public profile | нет player control | AI label, только публичные данные |
| sync | snapshot/events cursor | WS subscribe token | private scope, gap/resnapshot |
| assets | manifest/versioned files | нет player write | content hash, supported format |
| vip | own entitlement | нет paid mutation в срезе | серверное expiry |
| admin | все управляемые сущности | catalog/config/events/assets/VIP/roles | RBAC, audit, version, effective_at |
| test-admin | own test commands/history | grant money/resource, set own test state | спецроль, environment flag, reason, audit |

Первый рынок исполняет существующую заявку по её цене. Пользовательская команда place не обещает исполнение; accept указывает order_id, quantity и максимальную итоговую стоимость; сервер заново проверяет остаток, стоимость и срок. Защита от неблагоприятного изменения — max_total, не доверенная client_unit_price.

## Ошибки и совместимость

400 malformed;401 no session;403 forbidden;404 недоступная/отсутствующая сущность без утечки владения;409 version/idempotency/state conflict;422 доменное ограничение;429 limit;503 временная недоступность. Стабильные коды: INSUFFICIENT_FUNDS, STOCK_UNAVAILABLE, CAPACITY_EXCEEDED, RECIPE_INACTIVE, CONTRACT_EXPIRED, STALE_VERSION. После timeout результат уточняется тем же ключом, а не новой независимой покупкой.

Совместимость: новые поля опциональны, старый клиент поддерживается в оговорённом окне; несовместимость — новая версия/минимальная поддерживаемая сборка. API fixtures проверяются Go и Godot; админка использует тот же доменный тип суммы, без JS Number для финансов. WS — доставка минимум один раз; клиент дедуплицирует event_id, истёкший cursor возвращает RESYNC_REQUIRED и снимок с согласованной границей.
