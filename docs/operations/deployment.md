# Развёртывание и эксплуатация (план)

Ничего не развёрнуто. F09 требует предоставленного VPS/домена/доступа через защищённую среду; секреты не спрашивать в публичном чате и не сохранять в Git.

Ubuntu24.04LTS; Compose services nginx/server/postgres, приложение непривилегированное, БД только во внутренней сети. Persistent volumes для PostgreSQL/ассетов, immutable image digest, healthcheck, restart policy и ограничение журналов. Dev локален; staging для приёмки отдельно от prod, отдельные базы/учётки/hostname/ключи. .env.example содержит только имена/фиктивные значения и создаётся на F00; рабочий .env, keystore и dumps игнорируются и хранятся вне Git.

Nginx HTTPS: сертификат по проверенному процессу, redirectHTTP→HTTPS, ограничения body upload/rate limits, безопасные headers, WS upgrade только нужному route. Go API timeouts/pool/body limits; /health/live без секретов, /health/ready проверяет минимальные зависимости, /metrics закрыт от public. Публичные порты только 80/443 и ограниченный административный доступ; PostgreSQL5432 не публикуется.

Деплой: backup checkpoint→проверка артефакта/миграции→совместимое обновление→readiness→API/admin/APK smoke→наблюдение. Ручной разрешённый запуск защищённого environment, не автоматическая выкладка любого main push. Откат образа при совместимой схеме; разрушительная миграция требует отдельного плана и восстановления, не магического down.

## Backup, monitoring, incident

Ежедневный base backup+WAL до off-host storage; целевой RPO≤15 мин/RTO≤4 ч, retention candidate7daily/4weekly, шифрование и отдельные учётные данные. Команда восстановления проверяется в изолированной базе и сверяет ledger/catalog/счётчики; наличие файла не считается проверкой. Предоставленное хранилище и расписание фиксируются в runbook, исходное значение не гарантия.

Метрики: p95 API latency, failed commands, db pool/locks, production tick lag, outbox depth, agent decision cost, диск, backup age, emission/negative balance anomalies. Начальные алерты: tick lag>2 интервала, backup stale>24 ч, диск>80%, нарушение финансового инварианта немедленно; пороги уточняются после теста. Логи JSON с request/operation/tick IDs, без токенов/паролей.

Incident: ограничить новые мутации→сохранить доказательства→оценить денежные инварианты→изолированное восстановление/replay→сверить→возобновить с опубликованной политикой сроков. Нельзя вручную подкручивать balance и скрывать след. RPO/RTO и recovery measured фиксируются в отчёте F09.
