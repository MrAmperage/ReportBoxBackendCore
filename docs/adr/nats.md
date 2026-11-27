# ADR 0005 — NATS и JetStream

[Архитектура сообщений](../messaging.md) · [API NATS](../nats.md)

| Решение | Мотивация |
|---|---|
| Нативный NATS client (версия в `go.mod`) | Cancellable pull и сохранение нативного API. |
| Один endpoint, без discovery, reconnect 60 попыток, без reconnect buffer | Явная политика подключения, предотвращение скрытой буферизации; cluster failover позже по необходимости. |
| TLS/mTLS, token или credentials file, plaintext только по явной политике | Без передачи credentials в URL; ACL — deployment. |
| Нативная публикация через `Conn` / `JetStream`, managed registrations через Fx | Не дублировать API библиотеки; жизненный цикл управляемых подписок един для Core. |
| Existing durable pull, Fetch(1) на worker, 3s pull context, explicit DoubleAck и delayed NAK | Контролируемая concurrency, at-least-once, отдельная отмена pull и обработки. |
| Finite MaxDeliver, bounded MaxAckPending, AckWait/BackOff validation | Не допускать бесконечный retry и очевидно несовместимую remote policy. |
| Readiness по connection / AccountInfo / worker last result | Быстрый локальный статус и проверка доступности API, без создания/изменения streams. |
| Sticky readiness failure при permission violation / max subscriptions | Асинхронный callback не позволяет надёжно определить конкретного потребителя; не считать reconnect гарантией прав. Восстановление после исправления политики и перезапуска. |
| ACK/NAK failure снижает readiness; восстановление после успешного pull | Info не доказывает доступность доставки. Ошибка бизнес-обработки с успешным NAK не равна сбою транспорта. |
| Не выполнять Fetch во время известного reconnect | Сократить создание inbox при недоступном брокере; окно гонки внутри native Fetch остаётся. |
| Drain общего connection в Close, после всех participant Drain | RPC и handlers могут публиковать и ack до закрытия брокера. |

Ограничения: нет автоматического InProgress, DLQ/quarantine/advisory processing, transactional outbox/inbox и cluster failover/multi-endpoint. Формат envelope принадлежит прикладным контрактам. Нативные async publish и unmanaged subscriptions требуют собственного lifecycle. Не менять production broker автоматически. Telemetry опционально инструментирует управляемые handlers. См. [план](../development.md).
