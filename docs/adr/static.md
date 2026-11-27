# ADR 0010 — Независимая раздача frontend

[Архитектура](../architecture.md) · [Static API](../static.md)

- BackendCore не привязан к реализации внешней маршрутизации. TLS termination, маршруты и сетевые ACL определяет deployment; библиотека не генерирует и не изменяет его конфигурацию.
- Опциональный `static.Module()` владеет отдельным HTTP listener и открывает только доверенный каталог готовой сборки. Не добавлять gateway, REST framework, frontend build pipeline или federation runtime.
- Использовать `net/http`, `os.OpenRoot` (Go >=1.24) и `http.ServeContent`. Каталог неизменяем на протяжении процесса; отклонять symlinks и специальные файлы при старте. SPA fallback opt-in, только для navigation; консервативное кеширование, без эвристики immutable.
- Default `127.0.0.1:8080`, plaintext non-loopback opt-in, отдельная конфигурация `STATIC_*`, lifecycle через Core. Без явного подключения не создаются порты или файлы. RPC/Management остаются независимыми.
- Loopback Host guard (421; IP listener или localhost с фактическим портом), non-loopback допускает DNS names по политике deployment. SPA не маскирует существующие каталоги и slash-suffixed файлы; неоднозначный Sec-Fetch-Mode отклоняет fallback. Admission синхронизирован с Quiesce; forced drain отменяет lifetime context и ожидает активные handlers в рамках общего deadline, с резервом на отмену. Это не гарантия прерывания зависшего filesystem I/O.

Ограничения: один root и listener на приложение; нет embed.FS, динамической замены root, TLS, CORS, reverse proxy, asset compression и отдельных метрик Static. Это сознательные границы модуля; расширять по прикладной потребности.
