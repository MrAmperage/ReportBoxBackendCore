# HTTP, frontend deployment и внешняя маршрутизация

[Архитектура](architecture.md) · [Static](static.md)

BackendCore не управляет внешним ingress, reverse proxy или его конфигурацией. TLS termination, выбор маршрутов и сетевые ACL принадлежат deployment. Сервисы независимы:

| Listener | Default | Назначение |
|---|---|---|
| `static.Module()` | `127.0.0.1:8080` | Готовые frontend-файлы, опциональный SPA fallback |
| `rpc.ServerModule()` | `127.0.0.1:9090` | ConnectRPC, gRPC, gRPC-Web |
| `management.Module()` | `127.0.0.1:7613` | Закрытые health/metrics |

Для публичного API явно перечислять разрешённые процедуры, не публиковать внутренние методы, reflection и management. Сохранять RPC method, canonical path, headers и trailers; native gRPC требует HTTP/2. Сервис проверяет credentials и права на операции самостоятельно. Не доверять входящим identity-заголовкам. Для service-to-service — TLS/mTLS и ACL; h2c только в доверенной изолированной сети. Same-origin предпочтителен; если origin отличается, согласовать RPC CORS allowlist и auth/CSRF. Static не добавляет CORS или API-проксирование; UI использует настроенные публичные RPC URL. См. [RPC](rpc.md).

## UI deployment

Владелец FrontendService собирает Angular в собственном pipeline и поставляет только публичные файлы в отдельном неизменяемом каталоге, например `/app/public`. Подключает `static.Module()` и указывает `STATIC_ROOT`, `STATIC_SPA=true` для Shell; для assets-only сервиса fallback отключён. Для приватной контейнерной сети явно разрешить `STATIC_ADDRESS=0.0.0.0:8080` и `STATIC_ALLOW_REMOTE=true`. Если upstream Static слушает loopback, внешний reverse proxy должен передать `Host`, соответствующий IP listener или `localhost` и его порту; иначе Static вернёт 421. Для non-loopback listener DNS names разрешены при явном opt-in. Наружу отдавать HTTPS средствами deployment; не публиковать plaintext в недоверенную сеть.

Для микрофронтендов можно хранить артефакты по `/microfrontends/<service>/<build-id>/...`; manifest и federation остаются вне ядра. Версионированный URL сохраняется для всех связанных chunks, включая lazy loads; публиковать новую директорию полностью, не перезаписывать её при обслуживании запросов. Rollback выполняется заменой версии контейнера/артефакта. Кеширование Static консервативно (`no-cache`), без автоматического immutable. Запрещены directory listing, скрытые файлы, source maps, symlinks в build tree, выход за root. Missing asset не заменяется HTML. См. [Static API](static.md).

Management не публикуется наружу. Для приватного мониторинга явно разрешить `MANAGEMENT_ADDRESS=0.0.0.0:7613`, `MANAGEMENT_ALLOW_REMOTE=true` и сетевые ACL. См. [Management](management.md), [Telemetry](telemetry.md).
