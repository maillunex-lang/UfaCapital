# Развёртывание и эксплуатация

## Что развёрнуто сейчас (12.11.2026)

Это временная схема без HTTPS, не приёмка F09.

- Сервер — служба systemd `ufacapital` на VPS, пользователь `ufacapital`, бинарник `/opt/ufacapital/ufacapitald`, миграции `/opt/ufacapital/migrations`, админка `/opt/ufacapital/admin`. Unit: [ufacapital.service](../../infrastructure/vps/ufacapital.service).
- PostgreSQL 18.6 — compose-проект `ufacapital`, порт только `127.0.0.1:5432`.
- Игра слушает `:8088` по HTTP (открыт в ufw). Всё под `/admin` отвечает 404 любому адресу, кроме 127.0.0.1 (`ADMIN_LOOPBACK_ONLY=1`), потому что пароль и cookie по HTTP передавать нельзя.
- Секреты только на сервере: `/opt/ufacapital.env`, `/opt/ufacapital-pg.env`, `/opt/ufacapital-admin.txt` (все root, 0600).

Выкатка: из чистого дерева нужного коммита запустить `bash infrastructure/vps/deploy.sh`. Скрипт собирает сервер и админку, ставит их, перезапускает службу, ждёт `/healthz` и печатает ревизию. Проверить ревизию позже: `go version -m /opt/ufacapital/ufacapitald | grep vcs`. Docker и чужие проекты скрипт не трогает.

Вход в админку, пока нет HTTPS:

1. На своём компьютере: `ssh -L 8088:127.0.0.1:8088 root@<адрес VPS>`.
2. На сервере: `cat /opt/ufacapital-admin.txt` — почта, пароль и ссылка `otpauth://`. Секрет из ссылки (`secret=`) добавить в приложение-аутентификатор.
3. Открыть `http://127.0.0.1:8088/admin/`.

Первого суперадмина создаёт команда `ufacapitald bootstrap-admin` (переменные `BOOTSTRAP_ADMIN_EMAIL`, `BOOTSTRAP_ADMIN_PASSWORD`, пароль от 16 символов). Она срабатывает один раз: при существующем суперадмине отказывает. Действие записывается в журнал аудита.

Не сделано: HTTPS и домен, Nginx, резервные копии и проверка восстановления, мониторинг и алерты, отдельный staging. Диск VPS занят на 91% при пороге алерта 80%.

## План

F09 требует предоставленного домена/доступа через защищённую среду; секреты не спрашивать в публичном чате и не сохранять в Git.

Ubuntu24.04LTS; Compose services nginx/server/postgres, приложение непривилегированное, БД только во внутренней сети. Persistent volumes для PostgreSQL/ассетов, immutable image digest, healthcheck, restart policy и ограничение журналов. Dev локален; staging для приёмки отдельно от prod, отдельные базы/учётки/hostname/ключи. .env.example содержит только имена/фиктивные значения и создаётся на F00; рабочий .env, keystore и dumps игнорируются и хранятся вне Git.

Nginx HTTPS: сертификат по проверенному процессу, redirectHTTP→HTTPS, ограничения body upload/rate limits, безопасные headers, WS upgrade только нужному route. Go API timeouts/pool/body limits; /health/live без секретов, /health/ready проверяет минимальные зависимости, /metrics закрыт от public. Публичные порты только 80/443 и ограниченный административный доступ; PostgreSQL5432 не публикуется.

Деплой: backup checkpoint→проверка артефакта/миграции→совместимое обновление→readiness→API/admin/APK smoke→наблюдение. Ручной разрешённый запуск защищённого environment, не автоматическая выкладка любого main push. Откат образа при совместимой схеме; разрушительная миграция требует отдельного плана и восстановления, не магического down.

## Backup, monitoring, incident

Ежедневный base backup+WAL до off-host storage; целевой RPO≤15 мин/RTO≤4 ч, retention candidate7daily/4weekly, шифрование и отдельные учётные данные. Команда восстановления проверяется в изолированной базе и сверяет ledger/catalog/счётчики; наличие файла не считается проверкой. Предоставленное хранилище и расписание фиксируются в runbook, исходное значение не гарантия.

Метрики: p95 API latency, failed commands, db pool/locks, production tick lag, outbox depth, agent decision cost, диск, backup age, emission/negative balance anomalies. Начальные алерты: tick lag>2 интервала, backup stale>24 ч, диск>80%, нарушение финансового инварианта немедленно; пороги уточняются после теста. Логи JSON с request/operation/tick IDs, без токенов/паролей.

Incident: ограничить новые мутации→сохранить доказательства→оценить денежные инварианты→изолированное восстановление/replay→сверить→возобновить с опубликованной политикой сроков. Нельзя вручную подкручивать balance и скрывать след. RPO/RTO и recovery measured фиксируются в отчёте F09.
