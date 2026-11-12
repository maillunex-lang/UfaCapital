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

## Android SDK и APK

SDK не входит в Git.

| Что | Результат |
|---|---|
| SDK | `/opt/android-sdk` |
| cmdline-tools | `latest`, ревизия 23.0.0 (`sdkmanager --list_installed`, код 0) |
| platform-tools | 37.0.1, `adb` есть |
| build-tools | 35.0.1, 36.0.0, 36.1.0 |
| platforms | android-35, android-36 |
| cmake | 3.10.2.4988404 |
| NDK | 28.1.13356709 и 29.0.14206865 (шаблон Godot 4.7.2 просит 29) |
| JDK | OpenJDK 17.0.20.1, `/usr/lib/jvm/java-17-openjdk-amd64` |
| Export templates | `~/.local/share/godot/export_templates/4.7.2.stable`, `version.txt` = `4.7.2.stable` |
| `android_source.zip` SHA-256 | `a15416b528efb0a19a7d187e4d9f4e5336d0689144b111329fb4bd12c4447ad3` |

`scripts/prepare-android-template.sh` ставит в `client/android/` (каталог в `.gitignore`) исходный шаблон с `android:usesCleartextTraffic="true"` и Gradle `-Xmx1024m`. Официальные `-Xmx4536m` на машине с 4 ГБ RAM были убиты OOM. Keystore отладки лежит вне Git: `/opt/android-sdk/ufacapital-debug.keystore`, alias `androiddebugkey`, CN=`UFACAPITAL Debug`. Пароль — стандартный отладочный пароль Godot, в репозиторий не записан.

Сборка: Godot `--headless --export-debug Android`, Gradle `assembleStandardDebug`, код 0.

| Поле APK | Значение |
|---|---|
| Файл | `client/build/UFACAPITAL-debug.apk` (в Git не входит) |
| Размер | 166312212 байт |
| SHA-256 | `b138267bc530f7d5171f8c8a6cdea08936f81008a22b414d5eb448f567ca1157` |
| package | `dev.ufacapital.game`, versionCode 1, versionName `0.1.0` |
| SDK | min 29, target 36, compile 36; landscape; `INTERNET`; cleartext разрешён |
| ABI | `arm64-v8a`, `x86_64` |
| Подпись | `apksigner verify`: v2, сертификат SHA-256 `da6d7387a95f8d0787a669144b049d93fff9c30e299b3018496ac968aa40f951` |

minSdk 29 задаёт сам Godot для рендерера `mobile` (Vulkan). `adb devices` после `adb start-server` пустой: телефона и эмулятора нет, APK на устройство не ставился. На телефоне адрес `127.0.0.1` — это сам телефон; нужен LAN-адрес сервера. Это debug-сборка, не публикация в магазин.

## Не подтверждено

- Android-проверки (`sdkmanager`, `aapt`, `apksigner`, экспорт APK) сделаны 11.11 в среде сборки Grok. На VPS Android SDK нет, там они не повторялись.
- `adb devices` пуст: APK на телефон не устанавливался. F10 не DONE.
- Обнаружение скиллов AI-хостами не проверялось.

## Среда VPS, 12.11.2026

Docker 29.1.3 и Compose 2.40.3 есть. `docker compose -f infrastructure/dev/docker-compose.yml config` с паролем-заглушкой в окружении завершился кодом 0; без пароля — кодом 1. Запуск и остановка базы проверены во временном проекте на порту 55439 (`SELECT version()` → PostgreSQL 18.6, `down -v` → код 0). Go 1.27.4, Godot 4.7.2, Java 21.0.13, Node v26.13.0 — Java и Node новее записанных выше версий среды сборки. Админка собирается на Node 26.13.0 (эта же версия в CI). Таблица команд — в [F00-T01](../../roadmap/tasks/F00-T01.md).

Локальная база для интеграционных тестов слушает только `127.0.0.1:5432`, пользователь `ufacapital`, trust. Это не конфигурация продакшена и не попадает в Git.
