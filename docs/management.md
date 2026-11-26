# Management HTTP

[К описанию проекта](../README.md) · [ADR 0007](adr/management.md)

## Подключение

```go
backendcore.Run(app.Info{Name: "reportbox-example"}, management.Module())
```

Опциональный `management.Module(settings ...management.Config)` принимает не более одной полной конфигурации. Без аргумента читает ENV. `DefaultConfig()`, `LoadConfig()` и `Config.Validate()` не открывают сокеты. `*management.Server` можно получить через Fx; `Address()` возвращает фактически занятый адрес после Open, пустую строку до Open и после Close. Модуль не зависит от RPC, NATS, PostgreSQL или telemetry; он использует существующий `health.Registry`.

## Конфигурация

| ENV | Default |
|---|---|
| `MANAGEMENT_ADDRESS` | `127.0.0.1:7613` |
| `MANAGEMENT_ALLOW_REMOTE` | `false` |
| `MANAGEMENT_READ_HEADER_TIMEOUT` | `2s` |
| `MANAGEMENT_REQUEST_TIMEOUT` | `5s` |
| `MANAGEMENT_IDLE_TIMEOUT` | `15s` |
| `MANAGEMENT_MAX_HEADER_BYTES` | `16384` |
| `MANAGEMENT_MAX_CONCURRENT` | `16` |

Адрес — явный IPv4/IPv6 с числовым портом (IPv6 в квадратных скобках); `:0` допускается с явным IP для тестов. Неявные wildcard и hostname не поддерживаются. Пустые/некорректные значения отклоняются. Явный Config полностью заменяет ENV модуля. Loopback разрешён и в production. Для приватной Docker network:

```env
MANAGEMENT_ADDRESS=0.0.0.0:7613
MANAGEMENT_ALLOW_REMOTE=true
```

Не публиковать этот порт через Docker `ports` или публичный Caddy. На loopback контейнера доступен только из его network namespace; для удалённого scrape нужна изолированная сеть, ACL и при необходимости отдельный защищённый транспорт. Сервер не реализует TLS, аутентификацию или публичный API. `AllowRemote` не обеспечивает безопасность сети сам по себе.

## Endpoints

| Path | 200 | 503 |
|---|---|---|
| `/livez` | Starting, Running, Stopping | Idle, Failed, Stopped |
| `/readyz` | Running и обязательные checks успешны | Не готов, неполный probe, overload или shutdown |
| `/healthz` | Running, полный probe, все checks успешны | Любой check failed (в том числе optional), incomplete, overload или shutdown |
| `/metrics` | Определяется подключённым metrics handler | Не Running, overload или shutdown |

Без metrics handler `/metrics` возвращает 404. `/healthz` — диагностический endpoint, не readiness/liveness: optional check может дать 503 только на `/healthz`. Проверки выполняются один раз на запрос, без отдельного readiness probe внутри `/healthz`. Реестр проверяет зависимости последовательно с общим бюджетом 2s; ожидание реестра входит в бюджет. Liveness вообще не запускает checks и не ждёт лимит дорогих запросов. При исчерпании общего лимита `/readyz`, `/healthz`, `/metrics` получают 503 без очереди; `/livez` не зависит от этого лимита.

Примеры JSON:

```json
{"status":"ok"}
```

```json
{"status":"unavailable","complete":true,"checks":[{"name":"postgres.pool","healthy":false}]}
```

`complete=false` означает, что все результаты получить не удалось или фаза уже не Running; список может быть частичным или пустым. Не возвращаются DSN, raw errors, stack trace и конфигурация. Имена checks — технические идентификаторы, не включать в них секреты или пользовательские данные. Метрики также не должны содержать чувствительных значений. Разрешены только GET/HEAD, без тела, query string и нестандартных путей; неизвестные пути 404, неверный метод 405, тело 400. Health-ответы JSON, ошибки маршрутизации стандартные текстовые. `Cache-Control: no-store`; нет pprof, CORS или произвольных routes. HTTP/1.1, ограничения header/read/write/idle и контекст запроса. Лимит активной работы не является лимитом TCP-соединений; сетевые ACL и cooperative deadlines обязательны.

## Lifecycle

`management.ParticipantName` = `management.server`. Open синхронно bind; Activate запускает Serve; до Running readiness=false. Quiesce закрывает admission дорогих запросов (liveness остаётся доступным до прекращения приёма HTTP); Drain вызывает Shutdown, резервирует часть общего дедлайна для принудительного Close и отмены запросов, ждёт активные handlers в пределах бюджета. Close закрывает listener даже при ошибке activation и очищает Address. Неожиданный выход Serve сообщает безопасную ошибку через `lifecycle.ReportFailure`. Как и остальные модули, не гарантирует остановку пользовательского handler, игнорирующего context. Не логируются произвольные ошибки HTTP transport; фатальный Serve сигнализируется явно.

## Подключение Telemetry

Модуль не зависит от Prometheus. Optional Fx provider возвращает `management.MetricsHandler{Handler: handler}` (обычный `http.Handler`, например будущий promhttp). Без provider `/metrics` отсутствует. Handler создаётся пассивно, должен учитывать request context; его зависимости должны жить до конца Drain всех участников. Не добавлять конкурирующие providers или глобальный metrics registry. Telemetry SDK и exporter реализуются следующим этапом.

Пример: [examples/management-demo](../examples/management-demo/README.md).
