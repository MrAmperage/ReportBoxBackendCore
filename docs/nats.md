# NATS и JetStream: API кандидата

[Архитектура сообщений](messaging.md) · [ADR 0005](adr/nats.md) · [Пример](../examples/messaging-demo/README.md)

## Подключение

```go
backendcore.Run(app.Info{Name: "reportbox-worker"},
    nats.Module(),
    jetstream.Module(),
    nats.ProvideSubscription(newNoticeHandler),
    jetstream.ProvideConsumer(newEventHandler),
)
```

`nats.Module()` самостоятелен; `jetstream.Module()` требует `nats.Module()`. Core без этих модулей не соединяется с брокером. Конструкторы пассивны. `nats.Module(cfg)` принимает один полный `nats.Config` вместо ENV; `nats.DefaultConfig()` даёт defaults. JetStream API timeout фиксирован на 5s при отсутствии caller deadline. Собственные зависимости обработчика получают обычным DI. Нативные типы импортировать с alias, например `native "github.com/nats-io/nats.go"`, `jsapi "github.com/nats-io/nats.go/jetstream"`.

## Конфигурация NATS

| ENV | Default |
|---|---|
| NATS_URL | `nats://127.0.0.1:4222` |
| NATS_ALLOW_PLAINTEXT | `false` |
| NATS_CREDENTIALS_FILE | пусто |
| NATS_TOKEN | пусто |
| NATS_CA_FILE | пусто (system trust) |
| NATS_CERT_FILE / NATS_KEY_FILE | пусто |
| NATS_CONNECT_TIMEOUT | `5s` |
| NATS_DRAIN_TIMEOUT | `5s` |
| NATS_RECONNECT_WAIT | `2s` |
| NATS_MAX_RECONNECT | `60` |

Один явно заданный endpoint, без userinfo, path, query, списка URL; discovery отключён (не переходим на неожиданные адреса, cluster failover не реализован). Для production и remote hosts plaintext требует явного разрешения; credentials по plaintext тоже требуют разрешения. TLS (`tls://`) использует системное доверие и дополнительный CA при наличии, min TLS 1.2, optional client certificate/key. Token и credentials file взаимоисключающие. Не логировать config, сырые ошибки драйвера, subjects с персональными данными или payload. Broker должен применять ACL. NATS подключается с timeout и ограниченным числом reconnect, нативный jitter; при disconnect readiness false, исчерпание reconnect вызывает `ReportFailure`. Асинхронные `ErrPermissionViolation` и `ErrMaxSubscriptionsExceeded` (включая обёрнутые ошибки) блокируют readiness до перезапуска: callback не всегда сообщает, относится ошибка к publish или конкретной подписке. Это консервативная политика; исправить ACL/лимит и перезапустить сервис. Обычный slow consumer не блокирует readiness навсегда. Буферизация новых публикаций в reconnect отключена. Нативный connect не принимает context напрямую: timeout ограничивает dial, startup context проверяется до и после; не гарантируется мгновенное прерывание DNS/TLS.

## Core NATS: публикация и подписка

`(*nats.Connection).Conn() (*native.Conn, error)` после Open, во время reconnect возвращает тот же connection; до Open и после Close — ошибка. Не закрывать его, не менять callbacks и не выполнять самостоятельный Drain. Native access предназначен для публикаций и расширений, самостоятельно созданные subscriptions требуют собственного lifecycle.

```go
nc, err := connection.Conn()
if err != nil { return err }
if err := nc.Publish("reportbox.demo.notice.v1", payload); err != nil { return err }
return nc.FlushWithContext(ctx)
```

Flush не является подтверждением доставки подписчику или хранения. Для управляемой подписки:

```go
func newNoticeHandler() nats.Subscription {
    return nats.Subscription{
        Subject: "reportbox.demo.notice.v1", Queue: "workers",
        Timeout: 2*time.Second, PendingMessages: 128, PendingBytes: 1<<20,
        Handle: func(ctx context.Context, msg *native.Msg) error {
            return processNotice(ctx, msg.Data)
        },
    }
}
```

Очередь необязательна, один последовательный callback на registration; `Timeout` кооперативный, limits положительные. Ошибки/panic логируются без содержимого сообщения; Core NATS не делает retry. Slow consumers могут потерять сообщения. Подписки регистрируются и flush выполняется при Activate; При startup проверяются успешный flush, подключение и ошибки политики (включая LastError), а не произвольная устаревшая ошибка транспорта. Admission только после Running. При quiesce подписки отписываются, буферизованные сообщения могут быть отброшены; активные callbacks завершаются до Close connection. Readiness проверяет подключение и зафиксированные ошибки политики, но не гарантирует наличие прав на каждый subject: ACL и slow consumer необходимо контролировать отдельно.

## JetStream: публикация

`(*jetstream.Client).JetStream() (jsapi.JetStream, error)` доступен после Open. При startup и readiness запрашивается AccountInfo (`$JS.API.INFO` должен быть разрешён ACL). Для надёжной публикации использовать синхронный API с context:

```go
js, err := client.JetStream()
if err != nil { return err }
ack, err := js.Publish(ctx, subject, payload, jsapi.WithMsgID(eventID))
if err != nil { return err }
// ack.Duplicate означает совпадение ID в окне дедупликации.
_ = ack
```

ID должен быть стабильным при retry; ошибки могут означать неопределённый результат. Успех подтверждает сохранение по политике stream, не бизнес-обработку. Нативные async publishes и custom subscriptions не управляются автоматически: если используются, владелец должен дождаться их завершения/очистки до Close. По умолчанию использовать sync Publish. Stream/consumer создаёт deployment, не библиотека.

## Durable consumers

```go
func newEventHandler() jetstream.Consumer {
    return jetstream.Consumer{
        Stream: "REPORTBOX_EVENTS", Durable: "transport-worker",
        Concurrency: 4, HandleTimeout: 10*time.Second,
        AckTimeout: 2*time.Second, RetryDelay: 3*time.Second,
        Handle: func(ctx context.Context, d jetstream.Delivery) error {
            return storeIdempotently(ctx, d.Data)
        },
    }
}
```

Регистрируется через `jetstream.ProvideConsumer`. Consumer должен существовать, быть durable pull с explicit ack, finite positive MaxDeliver, ограниченным MaxAckPending >= локальной concurrency. AckWait и все значения BackOff должны быть больше HandleTimeout + AckTimeout; MaxWaiting должен позволять concurrency, MaxRequestExpires не меньше 3s при установленном лимите. Размер сообщения ограничивается stream/server, а не библиотекой. Concurrency 1–128 на регистрацию, Fetch(1) с 3s context, независимый worker на слот. Учитывать суммарную concurrency всех реплик и запас под сетевые задержки. Duplicate `(Stream, Durable)` в одном процессе запрещён. При известном reconnect новые pull inboxes не создаются. При ошибках pull или транспорта ACK/NAK worker становится unhealthy, выдерживает паузу 1s и повторно проверяет remote config. Успешный Info сам по себе не восстанавливает readiness: необходим успешный pull (допускается пустой batch) без ошибки подтверждения. Readiness отражает последние наблюдения workers и AccountInfo, не абсолютную гарантию будущих pull/ack. Восстановление после reconnect требует интеграционной проверки. В upstream `nats.go v1.53.1` в `pullConsumer.fetch` обнаружен путь возврата после неудачной отправки pull без явной отписки от созданного inbox; проверка подключения уменьшает риск, но не устраняет гонку disconnect между проверкой и Fetch. Проверить этот сценарий на брокере, контролируя `NumSubscriptions()`, до production; при воспроизведении исправить через upstream или другой native pull API.

Delivery содержит subject, data, headers, metadata без методов ack. Возврат nil означает успешный устойчивый бизнес-результат; библиотека выполняет DoubleAck с timeout. Ошибка/panic/истечение handler timeout — NakWithDelay, если процесс ещё не отменил работу; при forced cancel сообщение оставляется без ack. Если DoubleAck завершился ошибкой, результат неопределён, поэтому повторная доставка допустима. Неудачный ACK/NAK снижает readiness; успешный delayed NAK после бизнес-ошибки сам по себе не считается транспортным сбоем. Конечный MaxDeliver не создаёт DLQ автоматически. Идемпотентность, обработка исчерпания попыток, quarantine/DLQ и outbox остаются ответственностью сервисов. Длительные handlers требуют согласованного AckWait; автоматический InProgress не реализован.

## Lifecycle

Участники `nats.connection`, `nats.subscriptions` (depends on connection), `jetstream.client` (depends on connection), `jetstream.consumers` (depends on client). Core открывает все ресурсы до Activate, затем Running. До Running Core callbacks не принимают работу, JetStream ждёт Running. Quiesce: отписка Core и отмена pull, активные handlers используют отдельный lifetime context. Drain ждёт handlers и DoubleAck. При истечении общего deadline отменяет активные contexts, Close принудительно завершает ресурсы. Только после Drain всех участников общий NATS connection закрывается (native Drain с ожиданием ClosedHandler, timeout → Close; ошибочный disconnect в процессе drain фиксируется как ошибка остановки). Все callbacks должны уважать context; некооперативный код невозможно принудительно остановить. Уже начатая публикация/ack может иметь неопределённый результат при сбое.

## Проверка

Unit: config/validation, remote policy, panic recovery, latch ошибок ACL/лимита, результаты ACK/NAK и forced cancel. Broker integration: Core subscription, native publish, ForceReconnect и повторная доставка Core-сообщения после reconnect (новая публикация), redelivery, dedup, подтверждение активного handler при shutdown. Opt-in через `NATS_TEST_URL` на отдельном тестовом брокере с JetStream. См. [пример](../examples/messaging-demo/README.md). Локальный Go 1.23.2 не позволяет собрать весь модуль Go 1.27; доступ к proxy отсутствует, Docker и nats-server не установлены. В ревизии `5651a5d` повторно выполнены config test с `-race`, config vet и gofmt для nats/jetstream; `go test ./...` остановился при загрузке Go 1.27 (DNS proxy.golang.org недоступен). Новые regression tests написаны, но не запущены. Полные тесты, ACL/TLS, reconnect (включая длительный outage и число inbox subscriptions), queue-group distribution и forced shutdown пока не подтверждены. Перед merge выполнить проверки [плана разработки](development.md).
