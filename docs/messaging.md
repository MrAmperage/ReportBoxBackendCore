# ConnectRPC, NATS и JetStream

[К описанию проекта](../README.md) · [ADR 0003](adr/edge.md)

## 1. Выбор транспорта

| Сценарий | Транспорт |
|---|---|
| Angular → публичный API | Connect + Protobuf через Caddy |
| Go → Go | ConnectRPC; предлагаемый default — gRPC поверх HTTP/2 напрямую |
| Некритичное уведомление | Core NATS |
| Событие, которое должно быть сохранено | JetStream |
| Фоновая задача с повторной доставкой | JetStream durable consumer |

Не вводить второй обязательный RPC-протокол через NATS. Kafka, RabbitMQ и Watermill не входят в базовый стек. Отдельные grpc-go server и gateway не требуются; обычные gRPC-клиенты могут вызывать совместимые ConnectRPC handlers. gRPC-Web поддерживается сервером, но не является обязательным browser transport.

## 2. RPC (реализация-кандидат)

Реализованный API, настройки и ограничения см. в [RPC](rpc.md) и [ADR 0004](adr/rpc.md). Ниже сохраняются общие требования; при расхождении API ориентироваться на код и профильный документ.

Единый пакет `rpc`, независимое подключение server/client (предлагаемые `rpc.ServerModule()` и `rpc.ClientModule()`; сигнатуры определить при реализации). Сервер управляет `net/http.Server`, listener и регистрацией generated handlers из Protobuf. Клиенты используют переиспользуемый настроенный HTTP transport/client; generated typed clients принадлежат сервисам. Не создавать новый транспорт на каждый RPC и не выполнять RPC в DI-конструкторах. ConnectRPC major version и совместимые версии генераторов Go/TypeScript выбрать на этапе RPC; не смешивать разные API поколений.

Сервис владеет `.proto` и generated code; ядро предоставляет инфраструктуру для регистрации handlers, interceptors, deadlines, limits и lifecycle, но не доменные методы. Protobuf package `reportbox.<service>.v1`; удалённые поля резервировать, не менять семантику и номера; breaking change — новая версия. Генерировать Go и TypeScript из одной версии контракта. Место публикации контрактов, Buf/protoc и service discovery уточнить отдельно. На старте достаточно явно заданных endpoint URLs.

Server: `RPC_ADDRESS=127.0.0.1:9090` (default кандидата), read-header/idle timeouts, limits сообщений и concurrency, TLS/mTLS или явно разрешённый h2c в изолированной сети. Поддержка HTTP/2 обязательна для нативного gRPC, для h2c можно использовать `http.Server.Protocols` при поддерживаемой Go. Проверить реальные сетевые вызовы, не только httptest. Для browser Connect использовать unary и server streaming; client/bidi streaming не обещать стандартному fetch-транспорту. Для внутренних Go-вызовов streaming возможен при подходящем HTTP/2. Не ставить единый короткий WriteTimeout на длительные streams; для каждого RPC/stream установить подходящий bounded deadline и ограничение размера сообщения. Retry только для идемпотентных операций, с backoff/jitter и общим deadline; login/изменяющие операции не повторять автоматически.

Interceptors: request/trace ID, structured logging, telemetry (опционально), recovery, authn/authz hooks. Публичные и внутренние protobuf services или процедуры явно разграничены; routing allowlist Caddy — дополнительная граница, не замена авторизации. Не доверять metadata identity от клиента; сервисная идентичность и TLS-политика требуют согласования до production. Reflection не публиковать. Проверку готовности удалённого сервиса выполнять явно при необходимости, а не считать создание клиента доказательством его доступности.

| Доменная ситуация | RPC code (Connect/gRPC) |
|---|---|
| Некорректный ввод | InvalidArgument |
| Не найдено | NotFound |
| Конфликт уникальности | AlreadyExists |
| Нет аутентификации | Unauthenticated |
| Недостаточно прав | PermissionDenied |
| Недоступна зависимость | Unavailable |
| Неожиданная ошибка | Internal (без внутреннего текста) |

Lifecycle: участник Open синхронно bind, Activate запускает Serve, Quiesce закрывает admission, Drain ждёт запросы через Shutdown с выделенным deadline, затем при необходимости Close и отмена lifetime contexts. Закрытие DB/NATS только после drain; фоновые ошибки Serve передаются lifecycle.ReportFailure. Для активных streams определить отдельный stop signal и тестировать завершение. Clients закрывают idle connections после завершения обработчиков; запросы отменяются собственными контекстами. Не открывать сокеты до startup; пассивные transport/client объекты допустимы до Open при отсутствии сетевых операций. Management HTTP остаётся независимым; gRPC health compatibility — опциональная возможность, не отдельный публичный listener.

Критерии готовности: два Go-сервиса с typed clients, Angular/TS unary и server streaming через Caddy, native gRPC interoperability по HTTP/2, отказ для запрещённых процедур, deadline/cancellation, limits, mapping ошибок, trace propagation, bind failure, drain и forced close. Кандидат API и версии определены в ветке RPC; полные проверки до merge обязательны.

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

Источники: [Connect Go](https://connectrpc.com/docs/go/getting-started/), [Connect Web](https://connectrpc.com/docs/web/getting-started/), [gRPC guides](https://grpc.io/docs/guides/), [NATS JetStream](https://docs.nats.io/concepts/jetstream), [JetStream consumers](https://docs.nats.io/nats-concepts/jetstream/consumers), [NATS Go](https://github.com/nats-io/nats.go).
