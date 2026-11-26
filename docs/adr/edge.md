# ADR 0003 — Caddy + ConnectRPC

[Базовый ADR](baseline.md) · [Архитектура](../architecture.md)

**Статус:** согласовано владельцем проекта, 26 ноября 2026 г.; спецификация 0.3. Кандидат RPC реализован, см. [ADR 0004](rpc.md); deployment ещё не начат. Этот ADR имеет приоритет над ADR 0001 в части RPC, HTTP, gateway и frontend.

## Решение

- Использовать имеющийся Caddy для TLS, reverse proxy и раздачи Shell/remotes. Сохранить действующий NaiveProxy и его custom Caddy build.
- Отказаться от KrakenD как обязательной инфраструктуры, а также от отдельного API Gateway/BFF на старте. При обоснованной потребности отдельный BFF возможен позже.
- ConnectRPC + Protobuf для публичного browser RPC и внутренних вызовов Go. Поддержка gRPC и gRPC-Web одним RPC server; не нужен отдельный grpc-go listener.
- Один пакет `rpc` вместо `grpcserver`/`grpcclient`; подключение server/client независимо. `net/http` остаётся реализацией транспорта. Не добавлять универсальный REST framework, frontend server или embed UI в BackendCore.
- Backend и frontend сохраняют согласованную версию, но поставляются как отдельные артефакты. Management HTTP — опциональный отдельный закрытый listener.
- NATS/JetStream, pgx, slog, Fx, OpenTelemetry и Prometheus без изменений. Core остаётся текущей основой; Go-код этим ADR не меняется.

## Мотивация и компромиссы

Caddy уже развёрнут; ConnectRPC позволяет не вводить отдельный REST-to-gRPC преобразователь, сохраняя Protobuf и типизированных клиентов. Caddy не выполняет бизнес-авторизацию и агрегацию RPC. Сервисы должны проверять права самостоятельно. Browser fetch не гарантирует client/bidi streaming; не обещать эту возможность публичному API. UI публикуется независимо от Go binary, поэтому нужно управлять совместимостью и rollback двух артефактов. Один сервер для Caddy/NaiveProxy/ReportBox создаёт общий failure domain; при росте нагрузки оценить разделение и репликацию.

## Предложения, подлежащие review при реализации

| Предложение | Этап |
|---|---|
| Go-to-Go default gRPC; браузер Connect; RPC server/client в `rpc` | RPC |
| Версия ConnectRPC и API, генерация Buf/protoc и TS, схема contracts/service discovery | RPC |
| Same-origin, явный allowlist публичных процедур, h2c только в изолированной сети, TLS/mTLS между хостами | RPC/deployment |
| Порты 9090 RPC и 8081 management внутри контейнера; host bindings на loopback при host Caddy | RPC/deployment |
| URL `/microfrontends/<service>/<build-id>/...`, immutable releases, manifest switch и retention | UI deployment |
| Federation mode, auth/CSRF, routing policy и реальная Caddy конфигурация | UI/RPC deployment |
| NATS envelope, provisioning, DLQ; миграции PostgreSQL; telemetry exporter | Соответствующие модули |

Точный порядок: Core (в main, полная проверка ещё не подтверждена) → RPC → NATS/JetStream → PostgreSQL → Management → Telemetry → release. UI deployment можно делать параллельно, но он не является Go-модулем. CI и полную проверку Core необходимо довести до результата до утверждения новых модулей.

См. [RPC и события](../messaging.md), [Caddy и UI](../http-frontend.md), [план разработки](../development.md).
