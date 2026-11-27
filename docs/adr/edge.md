# ADR 0003 — ConnectRPC и внешняя маршрутизация

[Базовый ADR](baseline.md) · [Архитектура](../architecture.md)

**Статус:** решение о ConnectRPC сохранено. Политика раздачи UI заменена [ADR 0010](static.md). Этот ADR имеет приоритет над ADR 0001 в части RPC и gateway; ADR 0010 определяет новый контракт Static.

## Решение

- ConnectRPC + Protobuf для публичного browser RPC и внутренних вызовов Go; поддержка gRPC и gRPC-Web одним listener.
- Один пакет `rpc`, server/client подключаются независимо. Не вводить обязательный REST-to-gRPC adapter, универсальный REST framework или gateway/BFF.
- Внешний TLS, маршрутизация публичных процедур и выбор reverse proxy находятся вне BackendCore. Не доверять неподписанным identity-заголовкам; авторизация принадлежит сервису. Management закрыт.
- UI и backend могут развёртываться независимо с совместимыми контрактами. Static предоставляет отдельный listener, не меняет RPC.
- NATS/JetStream, pgx, Fx, slog и Telemetry сохраняются.

Browser fetch не гарантирует client/bidi streaming. Для native gRPC необходим HTTP/2; h2c только в изолированной сети с явным разрешением. Версионирование контрактов, auth/CSRF и federation определяются прикладными сервисами. См. [RPC](../rpc.md), [Frontend deployment](../http-frontend.md), [Static](../static.md).
