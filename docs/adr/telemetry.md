# ADR 0008 — Telemetry

[API](../telemetry.md) · [Архитектура](../architecture.md)

| Решение | Причина |
|---|---|
| OTel API/SDK, OTLP proto, Prometheus client (версии в `go.mod`) | Явный provider, стандартный формат экспорта и нативные API |
| Registry per-app, native `prometheus.Registerer` и `trace.TracerProvider` accessors | Без глобальных providers, native API для прикладного кода |
| `internal/observe.Instruments` как optional Fx dependency | Нет обязательного активного SDK в транспортах; один набор метрик, nil = no-op |
| OTLP/HTTP protobuf; официальный `otlptrace` transformer и небольшой явный HTTP client | Контроль TLS, redirects, deadlines, размера ответа и redaction; без неявной настройки транспорта и глобального error handler |
| Empty endpoint = metrics-only; sampling root ratio 0.1, bounded batch/queue | Нет обязательного collector; best effort, ошибки не блокируют readiness |
| RPC generated procedure + Connect code; остальные операции ограниченными labels | Не использовать динамические URL/subjects, SQL и payload; native publishers передают trace context явно |
| Лимит 256 RPC procedures на приложение и `_other` для overflow/invalid | Не допускать неограниченного роста series при динамических клиентах; сохранять существующие имена |
| Синхронное завершение streaming observations при Close RPC client | Не полагаться на асинхронный callback отмены при завершении provider; не ждать network I/O |
| OTLP Stop отменяет активные отправки и ждёт в пределах deadline | Соблюдать контракт exporter Client; при timeout не обещать завершение |
| RPC injector очищает прежние traceparent/tracestate перед Inject | Исключить повторное использование устаревших trace headers; unrelated headers сохраняются |
| Snapshots pgxpool/NATS, GORM logger | Нет дополнительных запросов к зависимостям, сохранены native API |
| Management optional provider, `promhttp` с safe gatherer | Отдельного listener нет; no telemetry → 404 |

Не реализованы OTLP/gRPC, auth/custom CA/mTLS к collector, автоматическая instrumentation native publishers и SQL spans; не утверждается exactly-once экспорт. User collectors обязаны ограничивать работу. Подробные ограничения в [Telemetry](../telemetry.md).
