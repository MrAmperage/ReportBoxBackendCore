# Caddy, микрофронтенды и management

[К описанию проекта](../README.md) · [ADR 0003](adr/edge.md)

## 1. Границы ответственности

Caddy выполняет TLS, статическую раздачу Angular Shell/remotes и reverse proxy публичных Connect RPC. KrakenD, Go-модуль frontend, embed.FS для UI и универсальный REST HTTP server исключены из базовой архитектуры. RPC server использует net/http внутри пакета `rpc`; это не дополнительный REST API. Management остаётся отдельным опциональным listener.

| Listener | Адрес (предложение) | Назначение |
|---|---|---|
| Caddy | :443 | Публичный HTTPS, статика, разрешённые RPC |
| RPC | :9090 внутри контейнера | Connect/gRPC/gRPC-Web |
| Management | :8081 внутри контейнера | /health/live, /health/ready, /metrics при telemetry |

Если Caddy работает на host, публиковать backend port только на loopback (например 127.0.0.1:9001 -> 9090); для Caddy в общей Docker network использовать внутреннее DNS-имя. Не пробрасывать RPC или management в интернет напрямую. Для разных машин — TLS/mTLS и сетевые ACL; h2c только в доверенной изолированной сети. Конкретные адреса задаёт deployment.

## 2. Маршрутизация RPC

Предлагаемый same-origin: Angular использует URL текущего origin; RPC URL имеет стандартный путь `/reportbox.auth.v1.AuthService/Login`. Caddy сохраняет method, path, headers и trailers согласно протоколу. Не использовать `handle_path` для удаления имени RPC service. Для gRPC upstream по cleartext требуется HTTP/2 (h2c); Connect unary работает и через HTTP/1.1. Если нужен TLS upstream, использовать HTTPS и проверять сертификат.

Иллюстративные фрагменты site block (не готовый Caddyfile и не замена существующей конфигурации NaiveProxy):

```caddyfile
route {
    @authPublic path /reportbox.auth.v1.AuthService/Login
    handle @authPublic {
        reverse_proxy h2c://127.0.0.1:9001
    }

    @rpcUnknown path_regexp rpcUnknown ^/reportbox\.
    handle @rpcUnknown {
        respond 404
    }
}

handle_path /microfrontends/* {
    root * /srv/reportbox/microfrontends
    header X-Content-Type-Options nosniff
    file_server
}
```

В примере Login — единственная явно разрешённая процедура. Не добавлять общий fallback reverse_proxy на RPC upstream: остальные RPC paths, включая служебные, не должны попадать на backend. В реальном deployment перечислить все публичные методы, либо выделить protobuf service, целиком предназначенный для публичного API; внутренние сервисы, reflection и health не проксировать наружу. Независимо от ingress каждый сервис проверяет credentials и права на каждую операцию; Caddy не является механизмом бизнес-авторизации. Не доверять пользовательским X-Forwarded-* или заголовкам identity. Для cookie auth определить CSRF policy; отключить CORS по умолчанию при same-origin. RPC-ошибки и trailers не превращать в HTML страницы.

Caddyfile необходимо адаптировать к имеющемуся кастомному build с forward_proxy, сохранив существующий порядок директив и probe resistance. Проверять `caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile` тем же бинарником, который используется в systemd; безопасно перезагрузить только после проверки. Не заменять binary стандартной сборкой без проверки наличия forward_proxy. Проверить прямой HTTPS, статику, RPC и работоспособность NaiveProxy после изменения. Не публиковать credentials и текущий Caddyfile с секретами в репозитории. Пример выше ещё не проверен на сервере.

## 3. Публикация микрофронтендов

Сервис владеет исходниками UI и его версией, но Go binary не раздаёт сборку. Pipeline собирает backend и Angular отдельно, публикует согласованные артефакты. Caddy читает только доверенное дерево статических ресурсов, без права записи. Предлагаемая структура:

```text
/srv/reportbox/
  shell/
  microfrontends/
    auth/<build-id>/remoteEntry.json
    auth/<build-id>/chunk-ABC.js
    reports/<build-id>/...
```

Shell получает remote URL из конфигурации/manifest. Версионированный URL должен сохраняться для всех связанных chunks/assets, включая lazy loads. Публиковать новую директорию полностью до переключения manifest; сохранять предыдущие версии на период совместимости. Не перезаписывать immutable URL. Rollback переключает manifest на совместимый frontend; это не заменяет совместимость backend API. Конкретный release/rollback workflow и срок хранения утверждаются при интеграции Shell. Native Federation или Module Federation остаётся открытым выбором; Caddy не зависит от формата сборки.

| Ресурс | Cache-Control (политика) |
|---|---|
| Изменяемый manifest, index.html, entry без build ID | no-cache |
| Доказанно immutable URL с build ID или content hash | public, max-age=31536000, immutable |
| Остальные | no-cache |
| Ошибки | no-store |

Не включать browse; исключить секреты, конфиги, source maps по умолчанию и symlinks за пределы root. Проверять только GET/HEAD для статики, MIME для JS/MJS/CSS/WASM/JSON, conditional GET и корректный 404. SPA fallback допустим только для маршрутов Shell, никогда для remote assets или RPC. Пример Caddy выше намеренно не содержит полной cache/SPA policy. Same-origin предпочтителен; при необходимости другого origin согласовать allowlist CORS и credentials.

## 4. Management (план)

Отдельный модуль на net/http без chi/Gin/Fiber. `GET /health/live` — 200 для живого процесса, `GET /health/ready` — 200 только при готовности, иначе 503; JSON без внутренних ошибок, Cache-Control: no-store. `/metrics` регистрируется при подключённой telemetry. Технические endpoints не публикуются через Caddy. Ограничить header/body/timeouts, методы и доступ к debug endpoints. Управление listener через lifecycle: Open синхронно bind, Activate запускает Serve, Quiesce прекращает intake, Drain выполняет Shutdown с дедлайном и Close при необходимости, Close освобождает listener; ошибки фонового Serve через ReportFailure. Readiness снимается до drain всех транспортов. Закрытие должно учитывать HTTP/2 streams; долгоживущие запросы имеют собственную политику завершения.

Источники: [Caddy reverse_proxy](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy), [handle](https://caddyserver.com/docs/caddyfile/directives/handle), [file_server](https://caddyserver.com/docs/caddyfile/directives/file_server), [Connect deployment](https://connectrpc.com/docs/go/deployment/).
