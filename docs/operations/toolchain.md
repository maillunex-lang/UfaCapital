# Toolchain первого среза

Проверено 11.11.2026 в среде сборки. Версии ниже — фактический вывод команд, не каталог «последних».

| Инструмент | Команда | Результат |
|---|---|---|
| Godot | `/opt/godot/Godot_v4.7.2-stable_linux.x86_64 --version` | `4.7.2.stable.official.ed1daf0bf` |
| Godot zip SHA-256 | релиз GitHub `4.7.2-stable` | `cadd3204e728a35d3f13adb7fd0d7902636b79f6b95c40c265eb73b6c35329e4` |
| Go | `go version` | `go1.27.4 linux/amd64` |
| Go tarball SHA-256 | официальный linux-amd64 | `7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840` |
| Node | `node -v` / `npm -v` | `v22.23.3` / `10.9.9` |
| JDK | `java -version` | OpenJDK `17.0.20.1` |
| PostgreSQL | `SELECT version()` | PostgreSQL `18.6` (Debian 18.6-1.pgdg13+2) |
| Образ Postgres | манифест tag `18` | `18.6`, sha256 `e923a22ac074261dd73970c57edf659475bf0497cb9456f17d98ed7cdd3ca913` |

Лицензии: Godot — MIT; Go — BSD-3-Clause; PostgreSQL — PostgreSQL License; OpenJDK — GPL-2.0 with Classpath Exception; Node.js — MIT.

## Не подтверждено

- `docker` и `docker compose` в этой среде отсутствуют (`command not found`). Файл [infrastructure/dev/docker-compose.yml](../../infrastructure/dev/docker-compose.yml) записан, команда `docker compose config` не выполнялась. Статус проверки: BLOCKED.
- `sdkmanager` и `adb` отсутствуют. Export templates Godot 4.7.2 не скачаны. Сборка APK: BLOCKED.
- Публичный VPS и физическое Android-устройство не предоставлены. Человеческая приёмка F10 не может быть DONE.

Локальная база для интеграционных тестов слушает только `127.0.0.1:5432`, пользователь `ufacapital`, trust. Это не конфигурация продакшена и не попадает в Git.
