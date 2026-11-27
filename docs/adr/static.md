# ADR 0010 — Независимая раздача frontend

[Архитектура](../architecture.md) · [Static API](../static.md)

Статус: направление согласовано владельцем 27 ноября 2026 г.; реализация — кандидат v0.1.1. Уточняет ADR 0003 только в части frontend и внешней маршрутизации. Не меняет существующие RPC API.

- BackendCore не привязан к реализации внешней маршрутизации. TLS termination, маршруты и сетевые ACL определяет deployment; библиотека не генерирует и не изменяет его конфигурацию.
- Опциональный `static.Module()` владеет отдельным HTTP listener и открывает только доверенный каталог готовой сборки. Не добавлять gateway, REST framework, frontend build pipeline или federation runtime.
- Использовать `net/http`, `os.OpenRoot` (Go >=1.24) и `http.ServeContent`. Каталог неизменяем на протяжении процесса; отклонять symlinks и специальные файлы при старте. SPA fallback opt-in, только для navigation; консервативное кеширование, без эвристики immutable.
- Default `127.0.0.1:8080`, plaintext non-loopback opt-in, отдельная конфигурация `STATIC_*`, lifecycle через Core. Без явного подключения не создаются порты или файлы. RPC/Management остаются независимыми.
- Версии зависимостей v0.1.0 не меняются. До публикации v0.1.1 проверить full build/test, интеграцию Fx, реальный listener, root containment и браузер. Нет изменения опубликованного тега v0.1.0.

Ограничения: один root и listener на приложение; нет embed.FS, динамической замены root, TLS, CORS, reverse proxy, asset compression и отдельных метрик Static. Это сознательные границы первого варианта; расширять по прикладной потребности.
