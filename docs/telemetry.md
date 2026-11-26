# Telemetry: OpenTelemetry + Prometheus

[К архитектуре](architecture.md) · [ADR 0008](adr/telemetry.md)

## Подключение

```go
backendcore.Run(app.Info{Name: "reportbox-auth-service"},
    management.Module(),
    telemetry.Module(),
)
```

Модуль опционален, `management` также не обязателен. При подключении Management метрики доступны на `http://127.0.0.1:7613/metrics`; без него Telemetry не открывает listener. Без Telemetry `/metrics` отсутствует. `telemetry.Module()` не делает глобальных `otel.Set*`, `prometheus.DefaultRegisterer` не меняется. Без Telemetry API транспорта работает как прежде; в общем Go module присутствуют зависимости OTel API и Prometheus client, а SDK/экспортер не создаются.

## Конфигурация

`telemetry.Module()` читает ENV; `telemetry.Module(cfg)` принимает полную конфигурацию, начиная с `telemetry.DefaultConfig()`.

| ENV | По умолчанию | Назначение |
|---|---|---|
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | отсутствует | Полный URL, приоритет; явно пустой — metrics-only |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | отсутствует | Base URL, добавляется `/v1/traces` |
| `OTEL_EXPORTER_OTLP_PROTOCOL` / `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` | `http/protobuf` | Только HTTP/protobuf, иные значения отклоняются |
| `OTEL_TRACES_SAMPLER` | `parentbased_traceidratio` | Только указанный sampler |
| `OTEL_TRACES_SAMPLER_ARG` | `0.1` | Root sampling ratio 0..1, parent sampled flag сохраняется |
| `TELEMETRY_ALLOW_PLAINTEXT` | `false` | Явное разрешение HTTP |
| `TELEMETRY_EXPORT_TIMEOUT` | `5s` | Максимальный бюджет одного HTTP export |

Пустой endpoint — metrics-only, tracer provider no-op; collector не нужен. Пример для закрытой сети с явным разрешением plaintext:

```env
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
TELEMETRY_ALLOW_PLAINTEXT=true
OTEL_TRACES_SAMPLER_ARG=0.1
```

HTTPS использует системные доверенные CA, TLS >= 1.2; credentials в URL, query, fragment и redirect запрещены. Нет автоматических proxy, auth headers, client certificates, OTLP/gRPC, custom CA или retry в этой версии. Не считать остальные `OTEL_*` настройками данного клиента; стандартный OTel SDK может читать собственные параметры span limits и внутренней диагностики. Не передавать секреты в названиях, атрибутах, пути endpoint и метриках.

## Встроенные метрики

| Имя | Тип | Labels |
|---|---|---|
| `reportbox_rpc_requests_total` | counter | direction, procedure, code |
| `reportbox_rpc_request_duration_seconds` | histogram | direction, procedure |
| `reportbox_operations_total` | counter | operation, outcome |
| `reportbox_operation_duration_seconds` | histogram | operation |
| `reportbox_nats_connected` | gauge | — |
| `reportbox_nats_messages_sent_total`, `reportbox_nats_messages_received_total`, `reportbox_nats_reconnects_total` | counters | — |
| `reportbox_postgres_connections_acquired`, `_idle`, `_total`, `_max` (префикс `reportbox_postgres_connections`) | gauges | — |
| `reportbox_postgres_acquires_total`, `reportbox_postgres_empty_acquires_total`, `reportbox_postgres_canceled_acquires_total` | counters | — |
| `reportbox_postgres_acquire_seconds_total` | counter | — |
| `go_*`, `process_*` | native collectors | стандартные |

`procedure` — статический generated RPC spec, не URL. Допускается не более 256 различных имён процедур на экземпляр приложения, каждое до 256 байт в формате Protobuf service/method. Некорректные и превышающие лимит имена объединяются в `_other` (метрики и имена spans). Лимит общий для client/server, не вытесняет уже зарегистрированные имена. `direction` — client/server, `code` — ограниченные Connect codes. RPC метрики включают выполненные interceptors; transport rejections до dispatch не учитываются. Streaming измеряется по всей длительности вызова, завершение при Receive EOF, ошибке, cancellation или CloseResponse; преждевременное закрытие — canceled. При Close RPC client синхронно завершает зарегистрированные streaming observations до закрытия provider; ожидание network I/O при этом не производится. Клиент обязан закрывать stream и соблюдать context: незакрытые streams могут удерживать состояние до завершения контекста или клиента. Новые вызовы во время shutdown не поддерживаются.

`operation`: `nats.receive`, `jetstream.process`, `jetstream.ack`, `jetstream.nak`, `gorm.query`; `outcome`: ok/error/canceled/retry/not_found. JetStream process измеряет business handler отдельно от ack/nak. GORM logger измеряет ORM-операции (в том числе ошибки и record-not-found), не оценивает SQL formatter; нативный pgx учитывается только через pool stats. Native NATS stats включают служебные сообщения; счётчики могут сброситься после остановки/смены соединения. Scrape читает snapshots без запросов к БД/брокеру; собственные метрики приложения не должны блокироваться или раскрывать данные. Максимум 2 одновременных Prometheus gathers; Management отдельно ограничивает admission, произвольный пользовательский collector невозможно отменить принудительно.

## API прикладного сервиса

Fx предоставляет `*telemetry.Runtime`. `Registerer() prometheus.Registerer` доступен при построении графа; `TracerProvider() trace.TracerProvider` возвращает native provider после Open при включённом export (до Open, после Close и в metrics-only — no-op). Не кешировать provider до startup. Для явной зависимости lifecycle использовать `telemetry.ParticipantName`. `Propagator()` возвращает W3C TraceContext без baggage.

Пример для собственного span и native publish (подключены NATS и Telemetry):

```go
ctx, span := runtime.TracerProvider().Tracer("auth").Start(ctx, "publish user created")
defer span.End()
msg := nats.NewMsg("reportbox.events.auth.user.created.v1")
msg.Data = data
runtime.Propagator().Inject(ctx, propagation.HeaderCarrier(http.Header(msg.Header)))
err := conn.PublishMsg(msg)
```

Здесь `nats` — native `github.com/nats-io/nats.go`, `conn` — управляемое native соединение. JetStream `PublishMsg` использует тот же carrier. Автоматически инструментируются только Core-managed handlers и RPC; native publish не обёрнут и требует явного span/Inject. Встроенный RPC injector заменяет все варианты регистра traceparent/tracestate, в том числе удаляет старые значения при отсутствии валидного SpanContext; остальные заголовки не трогает. Native publishers используют явный propagator и сами отвечают за очистку повторно используемых headers. Не копировать baggage или произвольные request headers. `slog.InfoContext` и другие контекстные вызовы добавляют Trace ID/Span ID при валидном SpanContext.

## Lifecycle, отказ и безопасность

DI-конструкторы не подключаются к collector; batch processor создаётся при Open. Core сначала quiesce/drain handlers, затем Close зависимостей; instrumented participants явно зависят от Telemetry. Очередь 2048 spans, batch 256, интервал 1s, без блокирующей постановки; при переполнении/отказе traces могут теряться. Ошибка export не влияет на Health; SDK Shutdown пытается отправить остаток в общем дедлайне. OTLP Stop запрещает новые отправки, отменяет активные HTTP-запросы и ждёт их завершения в пределах stop context. При таймауте возвращается ошибка, а не подтверждение завершения. Response body до 64 KiB, редактирование ошибок collector. Не гарантируется экспорт поздних spans при forced shutdown или клиенте, игнорирующем отмену. Parent-based sampling принимает флаг входящего trace context; для публичного ingress выбирать политику доверия trace headers и rate limits на границе. Нет глобального error handler override; ошибки нашего exporter передаются SDK только в безопасном виде.

Registry, custom collectors и spans могут содержать данные, добавленные приложением; сервис отвечает за их фильтрацию и закрытость management. Не публиковать `/metrics` через внешний Caddy. Для Prometheus в соседнем Docker container явно разрешить `MANAGEMENT_ADDRESS=0.0.0.0:7613`, `MANAGEMENT_ALLOW_REMOTE=true` и ограничить сеть; не публиковать порт на host.

[Пример](../examples/telemetry-demo/README.md). [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/) · [Prometheus Go client](https://github.com/prometheus/client_golang).
