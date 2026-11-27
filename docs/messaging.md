# Межсервисные контракты и сообщения

[RPC](rpc.md) · [NATS и JetStream](nats.md) · [ADR RPC](adr/rpc.md) · [ADR NATS](adr/nats.md)

## Выбор транспорта

| Сценарий | Транспорт |
|---|---|
| Angular → публичный API | Connect + Protobuf |
| Go → Go | ConnectRPC, предпочтительно gRPC поверх HTTP/2 |
| Некритичное уведомление | Core NATS |
| Устойчивое событие или задача | JetStream durable consumer |

Не вводить второй обязательный RPC transport через NATS. Сервис владеет `.proto` и generated code; package `reportbox.<service>.v1`. Удалённые поля резервировать; не менять номера и семантику; breaking change требует новой версии контракта. Go и TypeScript генерировать из одной версии. Выбор registry контрактов, генераторов и service discovery остаётся за прикладными сервисами. RPC API, лимиты и конфигурация находятся в [профильной документации](rpc.md). Retry только для идемпотентных операций с backoff/jitter и общим deadline; не повторять login и изменения состояния автоматически. Внешний allowlist не заменяет проверку прав; не доверять identity metadata от клиента.

| Доменная ситуация | RPC code (Connect/gRPC) |
|---|---|
| Некорректный ввод | InvalidArgument |
| Не найдено | NotFound |
| Конфликт уникальности | AlreadyExists |
| Нет аутентификации | Unauthenticated |
| Недостаточно прав | PermissionDenied |
| Недоступна зависимость | Unavailable |
| Неожиданная ошибка | Internal (без внутреннего текста) |

## Надёжность сообщений

Core NATS не сохраняет сообщения для отсутствующих подписчиков. Queue groups распределяют работу, но не создают устойчивую очередь. Успешный flush не означает durable storage.

JetStream использует существующие durable pull consumers, explicit ack и ограниченную конкурентность. Provisioning stream/consumer, retention, quotas, storage и replicas определяет deployment. Ack после устойчивого результата; ошибка обработчика вызывает delayed NAK. AckWait/MaxDeliver и MaxAckPending должны соответствовать нагрузке и времени обработки. Длительная обработка требует достаточного AckWait; автоматический InProgress не реализован. При отмене обработки результат может быть неизвестен, повторная доставка допустима. MaxDeliver не создаёт DLQ сам; для quarantine/DLQ требуется отдельная прикладная политика. Успех publish означает подтверждение хранения, не выполнения handler.

At-least-once допускает дубли. `Nats-Msg-Id` подавляет повторные публикации только в окне дедупликации; бизнес-обработчики идемпотентны по event ID. При необходимости применять transactional inbox/уникальный ключ. Для согласованного изменения БД и события — transactional outbox и relay со стабильным ID; ядро не предоставляет атомарность PostgreSQL + NATS и не реализует outbox/inbox. Параллелизм и redelivery не гарантируют порядок завершения handlers. Проверять crash между commit/publish/ack, reconnect и повторную доставку.

## Соглашения о контрактах

Рекомендуемая структура subjects:

```text
reportbox.events.auth.user.created.v1
reportbox.events.reports.report.completed.v1
reportbox.jobs.reports.generate.v1
```

Lowercase, токены разделены точкой, версия последняя. Event — факт, job — действие. Не включать персональные данные или секреты в subject. Environment отделять аккаунтом/кластером или явно согласованным namespace. Независимые consumers события имеют отдельный durable name, реплики одного обработчика — общий. LimitsPolicy для fan-out и WorkQueuePolicy для непересекающихся задач выбираются при provisioning, а не задаются ядром.

У события должен быть стабильный ID для дедупликации, versioned payload, producer и UTC timestamp; correlation/causation ID по потребности. W3C trace context передаётся в `traceparent`/`tracestate`, `Nats-Msg-Id` используется для dedup. Универсальный protobuf envelope не утверждён и не реализован; его необходимость определяется отдельно. Не добавлять reflection dispatcher без прикладной потребности.
