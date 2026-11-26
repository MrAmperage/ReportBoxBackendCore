# Хранилище, наблюдаемость и безопасность

[К описанию проекта](../README.md)

## 1. PostgreSQL

`postgres.Module()` предоставляет пассивного владельца пула, создаёт `*pgxpool.Pool` в startup, выполняет `Ping` с ограниченным timeout (создание пула само по себе не доказывает доступность БД) и закрывает пул после завершения активных обработчиков. Нативный pool доступен через accessor после успешного открытия ресурса (включая startup зависимых компонентов и drain до закрытия); при ошибке Ping частично созданный пул закрывается. Предлагаемые настройки: `POSTGRES_DSN` (обязателен), `POSTGRES_MAX_CONNS=10`, `POSTGRES_MIN_CONNS=0`, `POSTGRES_CONNECT_TIMEOUT=5s`, `POSTGRES_HEALTH_TIMEOUT=2s`; значения pool настраиваются по бюджету подключений всех реплик. TLS и сертификаты задаются DSN/deployment, для production не отключать проверку TLS без согласованного защищённого транспорта. DSN никогда не выводится целиком.

Миграции принадлежат сервису, выполняются отдельным deployment job/CLI с контролируемым lock, не каждым экземпляром при старте. Ядро не создаёт прикладные таблицы и не навязывает ORM. GORM может подключаться сервисом отдельно при обосновании; pgx является базовым вариантом. Транзакции и isolation выбираются приложением. Использовать context, параметры SQL, корректное закрытие rows; тестировать rollback, timeout и отмену. Не читать чужую БД напрямую вместо API/event-контракта.

Readiness обязательной БД проверяется с коротким deadline; liveness не зависит от БД. На остановке сначала дожидаемся handlers, потом закрываем pool. `pgxpool.Close()` не принимает context и может ждать возврата соединений: все запросы, rows, acquired connections и транзакции должны иметь управляемый lifetime. Отмена контекста не заменяет явный Release/Close/Rollback. Для rollback использовать ограниченный контекст, если рабочий уже отменён. Если пул не завершился в срок, зафиксировать ошибку и не утверждать, что освобождение гарантировано. Для CLI окончательный предел — выход процесса; при embedded `New` ответственность за завершение остаётся у хоста.

## 2. Observability

`telemetry.Module()` опционален. Использует OpenTelemetry SDK для traces, стандартные W3C propagators и OTLP exporter; Prometheus endpoint на management listener. При отсутствии модуля транспорт использует no-op tracing и отключённые метрики, а не требует регистрации SDK. Предлагаемая точка расширения — явные provider/propagator и опциональный metrics registerer; не создавать конкурирующие Fx providers одного типа в core и telemetry. Management получает metrics handler только при подключении telemetry; registry не зависит от наличия HTTP listener. Сервис не зависит от доступности коллектора. Не считать отказ экспортера причиной liveness/readiness, если это явно не требуется политикой. Экспортер не блокирует приложение бесконечно, буферы ограничены. На остановке flush/shutdown выполняются с дедлайном.

Resource attributes: `service.name`, `service.version`, `service.instance.id` (если доступен), `deployment.environment.name`. Предлагаемые настройки: стандартные `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG`, явная политика TLS. Tracing: ConnectRPC client и server (включая gRPC protocol), management HTTP при необходимости, JetStream publish/process, при необходимости DB; пропагация через headers/metadata. Не переносить credentials через baggage, не захватывать тело запросов и query values по умолчанию. Использовать явные provider/registry для тестов вместо неуправляемого global state.

Metrics (имена проектные):

| Метрика | Тип | Labels (ограниченный набор) |
|---|---|---|
| `http_requests_total` | Counter | method, route, status_class |
| `http_request_duration_seconds` | Histogram | method, route |
| `rpc_requests_total` | Counter | service, method, protocol, code |
| `rpc_request_duration_seconds` | Histogram | service, method, protocol |
| `nats_connected` | Gauge | — |
| `jetstream_messages_total` | Counter | consumer, outcome |
| `jetstream_handler_duration_seconds` | Histogram | consumer |
| `postgres_pool_connections` | Gauge | state |

Конкретные имена сверяются с выбранной instrumentation, чтобы не дублировать встроенные метрики. Не использовать user_id, UUID, произвольные URL, message IDs или тексты ошибок как labels. Metrics endpoint защищён сетью; отключение или отсутствие telemetry не ломает management. Логи, traces и метрики связываются через request/trace ID, но не через неограниченные metric labels.

## 3. Безопасность по умолчанию

- Least privilege: отдельные учётные данные PostgreSQL и NATS для сервисов, ограниченные grants/subjects, ротация. Не хранить секреты в репозитории или образе.
- TLS на внешнем Caddy ingress; только разрешённые публичные RPC маршрутизируются наружу; для service-to-service — явная политика TLS/mTLS, доверенных сертификатов и сетевых ACL. NATS — TLS и авторизация subject. Не принимать идентичность из неподписанных заголовков gateway.
- Ядро предоставляет точки подключения auth middleware/interceptors, но не реализует бизнес-авторизацию или универсальный JWT policy. AuthService/потребители владеют проверкой прав.
- Ограниченные request sizes, deadlines, concurrency и queues. Recovery на transport boundary не скрывает неожиданные ошибки; клиент получает безопасное сообщение, журнал — достаточные данные без секретов.
- Контейнер non-root, минимальные разрешения, read-only FS при возможности; health/metrics не доступны из публичной сети.
- Не включать небезопасные development defaults незаметно в production. При явно выбранном production-профиле проверять обязательные security-настройки подключённых модулей.

Источники: [pgxpool](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool), [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/), [Prometheus Go client](https://github.com/prometheus/client_golang).
