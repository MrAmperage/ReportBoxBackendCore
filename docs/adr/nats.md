# ADR 0005 — Реализация NATS и JetStream

[Архитектура сообщений](../messaging.md) · [API NATS](../nats.md)

Статус: кандидат для ветки NATS на базе `b077f03`; решения реализации предложены агентом, подлежат review. Не считать протестированным релизом.

| Решение | Мотивация |
|---|---|
| `github.com/nats-io/nats.go v1.53.1`, Go 1.27 без изменений | Версионированный native client и cancellable FetchContext; зависимости проверены по go.mod тега, полная сборка ещё не выполнена. |
| Один endpoint, без discovery, reconnect 60 попыток, без reconnect buffer | Явная политика подключения, предотвращение скрытой буферизации; cluster failover позже по необходимости. |
| TLS/mTLS, token или credentials file, plaintext только по явной политике | Без передачи credentials в URL; ACL — deployment. |
| Нативная публикация через `Conn` / `JetStream`, managed registrations через Fx | Не дублировать API библиотеки; жизненный цикл управляемых подписок един для Core. |
| Existing durable pull, Fetch(1) на worker, 3s pull context, explicit DoubleAck и delayed NAK | Контролируемая concurrency, at-least-once, отдельная отмена pull и обработки. |
| Finite MaxDeliver, bounded MaxAckPending, AckWait/BackOff validation | Не допускать бесконечный retry и очевидно несовместимую remote policy. |
| Readiness по connection / AccountInfo / worker last result | Быстрый локальный статус и проверка доступности API, без создания/изменения streams. |
| Drain общего connection в Close, после всех participant Drain | RPC и handlers могут публиковать и ack до закрытия брокера. |

Открытые вопросы: автоматический InProgress для длительных jobs, DLQ/quarantine/advisory processing, transactional outbox/inbox, контракт envelope, cluster failover/multi-endpoint, telemetry. Нативные async publish и unmanaged subscriptions требуют собственного lifecycle. Не менять production broker автоматически. Полный go mod tidy и интеграционные проверки обязательны перед утверждением. См. [план](../development.md).
