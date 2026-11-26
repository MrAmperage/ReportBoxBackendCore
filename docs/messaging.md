# gRPC, NATS и JetStream

[К описанию проекта](../README.md)

## 1. Выбор транспорта

| Сценарий | Транспорт |
|---|---|
| Синхронный вызов метода другого сервиса | gRPC + Protobuf |
| Некритичное уведомление, допустима потеря | Core NATS |
| Событие, которое должно быть сохранено | JetStream |
| Фоновая задача с повторной доставкой | JetStream durable consumer |

Не вводить второй обязательный RPC-протокол через NATS. Core NATS request/reply может появиться позже при подтверждённой потребности. Kafka, RabbitMQ и Watermill не входят в базовый стек.

## 2. gRPC

`grpcserver.Module()` управляет экземпляром `grpc.Server`, listener и health service. `grpcclient.Module()` предоставляет управление именованными клиентскими соединениями; создание и закрытие привязаны к lifecycle. Конструкторы прикладного кода не выполняют RPC до старта. Серверные регистрации выполняются до `Serve`. Сервис владеет своими protobuf definitions, generated code и mapping доменных ошибок.

Предлагаемые настройки: `GRPC_ADDRESS=:9090`, отдельные limits на размер send/receive, keepalive и timeout по требованиям сервиса. Клиентские адреса задаются потребителем; не создавать клиент по каждому запросу. Использовать long-lived ClientConn. Не считать факт создания ClientConn доказательством подключения/здоровья remote. Startup/readiness критичного remote проверяются явно, а не для всех клиентов без исключения.

Клиент устанавливает deadline на каждый RPC. Retry — только при идемпотентности, ограниченное число попыток, backoff/jitter и общий deadline; без бесконечного retry. Interceptors: telemetry, structured logging, recovery для unary и stream, при необходимости authn/authz. Контекст отмены передаётся в repository и downstream. Не логировать полные запросы. TLS/mTLS и доверенные сертификаты задаются deployment; insecure допустим только явно для локальной разработки или согласованной защищённой сети. Reflection по умолчанию выключен в production.

| Доменная ситуация | gRPC status (пример) |
|---|---|
| Некорректный ввод | InvalidArgument |
| Не найдено | NotFound |
| Конфликт уникальности | AlreadyExists |
| Нет аутентификации | Unauthenticated |
| Недостаточно прав | PermissionDenied |
| Недоступна зависимость | Unavailable |
| Неожиданная ошибка | Internal (без внутреннего текста) |

Health `SERVING` только при готовности, перед остановкой `NOT_SERVING`; `GracefulStop()` ограничивается общим shutdown deadline, затем `Stop()`. Proto3-контракты: пакет `reportbox.<service>.v1`, не переиспользовать номера удалённых полей (`reserved`), не ломать семантику существующих полей, breaking changes — новая версия.

## 3. NATS Core

`nats.Module()` предоставляет одного пассивного владельца подключения на сервис; нативный `*nats.Conn` доступен через accessor после успешного открытия подключения, в том числе для запуска зависимых компонентов и drain до закрытия. JetStream context создаётся только после готовности подключения. Accessor до старта/после закрытия возвращает определённую ошибку, не nil pointer panic. Подключение устанавливается с ограниченным timeout в startup; callbacks учитывают disconnect/reconnect/closed. Reconnect с backoff/jitter и явной политикой; на временной потере обязательного подключения readiness=false. Не логировать credentials из URL. Настройки: `NATS_URL` (локально `nats://localhost:4222`), `NATS_CONNECT_TIMEOUT`, `NATS_DRAIN_TIMEOUT`, TLS и credentials через файлы/секреты. Не устанавливать wildcard permissions по умолчанию.

Core NATS не сохраняет сообщения для отсутствующих подписчиков. Не использовать его для событий, потеря которых нарушит бизнес-инварианты. Queue groups распределяют работу между активными подписчиками, не дают устойчивой очереди. Core publisher не обещает гарантии хранения сообщения, даже если flush успешен. При остановке сначала прекращать admission и доставку новой работы, затем дожидаться активных handlers (включая исходящие публикации и ack), после этого drain и закрытие подключения с дедлайном. Не закрывать общий connection до завершения consumers; при исчерпании бюджета закрыть connection принудительно и вернуть ошибку. Для Core NATS отдельно определить судьбу буферизованных сообщений; drain не делает их durable.

## 4. JetStream

`jetstream.Module()` требует NATS. Использовать актуальный API `github.com/nats-io/nats.go/jetstream`. Долговечные stream/consumer definitions управляются deployment или явной процедурой provisioning, не произвольным destructive update при каждом запуске. Параметры retention, max age/bytes, replicas, storage и quotas обязательны для production и определяются нагрузкой. Несовместимая конфигурация выявляется явно.

Рекомендуемый consumer: durable pull, explicit ack, ограниченные batch size, concurrency, AckWait и MaxAckPending. Для долгой обработки поддерживать lease/`InProgress` либо проектировать достаточно длинный AckWait. Ack только после успешного устойчивого результата. При shutdown остановить новые pull-запросы, учесть уже полученный буфер, позволить активным handlers завершиться и отправить ack при живом соединении. При timeout отменить handlers; сообщения с неопределённым результатом не ack, они могут быть доставлены снова. API `Stop`/`Drain` выбранного consumer проверить интеграционным тестом, а не считать оба одинаковыми. При необходимости подтверждённого ack использовать `DoubleAck(ctx)` с ограниченным временем; это не делает бизнес-операцию exactly-once. Ошибка временная — повтор с backoff; постоянная — контролируемая quarantine/DLQ. `MaxDeliver` сам по себе не переносит сообщение в DLQ: требуется отдельная обработка исчерпанных попыток или явная публикация с подтверждением перед Term/Ack. Ошибка публикации в quarantine не должна молча терять исходное сообщение.

At-least-once означает возможные дубли. `Nats-Msg-Id` помогает подавлять повторные публикации в пределах настроенного окна, но не гарантирует exactly-once бизнес-эффекты. Consumers идемпотентны по event ID, используют transactional inbox/уникальный ключ при необходимости. Успех durable publish означает подтверждение хранения брокером, не выполнение обработчиком. Глобальный порядок завершения handlers не гарантируется при параллелизме и redelivery. Если durable consumer обязателен для назначения сервиса, readiness учитывает состояние его работы, а не только подключение NATS; восстановление подписки/consumer после reconnect проверяется отдельно.

Для согласованного изменения БД и публикации события — transactional outbox в транзакции сервиса, затем relay с повторными попытками и стабильным event ID. Ядро может добавить общие примитивы позже; в первой версии не обещает атомарность PostgreSQL + NATS. Отдельно тестировать crash между commit/publish/ack.

## 5. Предлагаемый формат subject и контракта

```text
reportbox.events.auth.user.created.v1
reportbox.events.reports.report.completed.v1
reportbox.jobs.reports.generate.v1
```

Правила: lowercase, точка — разделитель токенов, версия последняя; event — факт в прошедшем времени, job — действие. Не включать пользовательские данные или секреты в subject. Environment разделять аккаунтами/кластерами либо явно согласованным namespace, не только строкой subject. Независимые consumers события имеют собственный durable name, реплики одного обработчика используют общий durable. События и jobs имеют разные retention-политики: для fan-out событий — LimitsPolicy как базовое предложение, для непересекающихся work queues — WorkQueuePolicy с учётом ограничений фильтров.

Предлагаемый envelope v1 (Protobuf, пока не утверждён):

```proto
syntax = "proto3";
package reportbox.events.v1;
import "google/protobuf/timestamp.proto";

message EventEnvelope {
  string id = 1;
  string type = 2;
  string producer = 3;
  google.protobuf.Timestamp occurred_at = 4;
  string correlation_id = 5;
  string causation_id = 6;
  string payload_type = 7;
  bytes payload = 8;
}
```

`id` уникален и стабилен при retry, `type` соответствует subject, `payload_type` идентифицирует protobuf message. UTC timestamp, tracing context в NATS headers (`traceparent`, `tracestate`), `Nats-Msg-Id` равен id для dedup. Payload contracts принадлежат сервисам; выбор envelope vs типизированные payload + headers предстоит утвердить. Не внедрять универсальный reflection-диспетчер без необходимости.

Источники: [gRPC guides](https://grpc.io/docs/guides/), [NATS JetStream](https://docs.nats.io/concepts/jetstream), [JetStream consumers](https://docs.nats.io/nats-concepts/jetstream/consumers), [NATS Go](https://github.com/nats-io/nats.go).
