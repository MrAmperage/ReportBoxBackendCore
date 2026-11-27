# ADR 0001 — Базовая архитектура

[Архитектура](../architecture.md) · [Core](core.md)

## Решение и мотивация

- BackendCore — один Go module, а не отдельный процесс или gateway. Один module graph и версионирование упрощают использование и проверку совместимости. Опциональные модули не открывают ресурсы без подключения.
- Uber Fx используется для композиции и DI; прикладной код не зависит от контейнера. Единый координатор обеспечивает управляемый lifecycle без второго DI framework.
- Typed Config, ENV и явные overrides позволяют валидировать настройки до открытия ресурсов без обязательного внешнего configuration framework.
- ConnectRPC + Protobuf — единый синхронный RPC transport; NATS Core — некритичные уведомления, JetStream — устойчивые события и jobs. Не вводить второй обязательный RPC transport, Kafka, RabbitMQ или Watermill без отдельной потребности.
- pgxpool и опциональный GORM используют один физический пул; slog, OpenTelemetry и Prometheus обеспечивают наблюдаемость без глобального состояния.
- Static, RPC и Management — независимые listeners. Маршрутизация, TLS termination внешнего ingress и сборка frontend принадлежат deployment и сервисам. Core не содержит доменных моделей, миграций или UI-компонентов.

Детали: [RPC](rpc.md), [Messaging](nats.md), [PostgreSQL](postgres.md), [Management](management.md), [Telemetry](telemetry.md), [Static](static.md).
