# Хранилище, наблюдаемость и безопасность

[К оглавлению](../README.md)

## 1. PostgreSQL

`postgres.Module()` создаёт `*pgxpool.Pool`, проверяет доступность с ограниченным timeout при старте, закрывает пул после завершения активных обработчиков. Предлагаемые настройки: `POSTGRES_DSN` (обязателен), `POSTGRES_MAX_CONNS=10`, `POSTGRES_MIN_CONNS=0`, `POSTGRES_CONNECT_TIMEOUT=5s`, `POSTGRES_HEALTH_TIMEOUT=2s`; значения pool настраиваются по бюджету подключений всех реплик. TLS и сертификаты задаются DSN/deployment, для production не отключать проверку TLS без согласованного защищённого транспорта. DSN никогда не выводится целиком.

Миграции принадлежат сервису, выполняются отдельным deployment job/CLI с контролируемым lock, не каждым экземпляром при старте. Ядро не создаёт прикладные таблицы и не навязывает ORM. GORM может подключаться сервисом отдельно при обосновании; pgx является базовым вариантом. Транзакции и isolation выбираются приложением. Использовать context, параметры SQL, корректное закрытие rows; тестировать rollback, timeout и отмену. Не читать чужую БД напрямую вместо API/event-контракта.

Readiness обязательной БД проверяется с коротким deadline; liveness не зависит от БД. На остановке сначала дожидаемся handlers, потом закрываем pool. pgxpool.Close может ждать возврата соединений: все запросы и транзакции должны иметь управляемый lifetime.

## 2. Observability

`telemetry.Module()` опционален. Использует OpenTelemetry SDK для traces, стандартные W3C propagators и OTLP exporter; Prometheus endpoint на management listener. При отсутствии модуля используются no-op telemetry primitives, сервис не зависит от доступности коллектора. Не считать отказ экспортера причиной liveness/readiness, если это явно не требуется политикой. Экспортер не блокирует приложение бесконечно, буферы ограничены. На остановке flush/shutdown выполняются с дедлайном.

Resource attributes: `service.name`, `service.version`, `service.instance.id` (если доступен), `deployment.environment.name`. Предлагаемые настройки: стандартные `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG`, явная политика TLS. Tracing: HTTP/gRPC client и server, JetStream publish/process, при необходимости DB; пропагация через headers/metadata. Не переносить credentials через baggage, не захватывать тело запросов и query values по умолчанию. Использовать явные provider/registry для тестов вместо неуправляемого global state.

Metrics (имена проектные):

| Метрика | Тип | Labels (ограниченный набор) |
|---|---|---|
| `http_requests_total` | Counter | method, route, status_class |
| `http_request_duration_seconds` | Histogram | method, route |
| `grpc_requests_total` | Counter | service, method, code |
| `grpc_request_duration_seconds` | Histogram | service, method |
| `nats_connected` | Gauge | — |
| `jetstream_messages_total` | Counter | consumer, outcome |
| `jetstream_handler_duration_seconds` | Histogram | consumer |
| `postgres_pool_connections` | Gauge | state |

Конкретные имена сверяются с выбранной instrumentation, чтобы не дублировать встроенные метрики. Не использовать user_id, UUID, произвольные URL, message IDs или тексты ошибок как labels. Metrics endpoint защищён сетью; отключение или отсутствие telemetry не ломает management. Логи, traces и метрики связываются через request/trace ID, но не через неограниченные metric labels.

## 3. Безопасность по умолчанию

- Least privilege: отдельные учётные данные PostgreSQL и NATS для сервисов, ограниченные grants/subjects, ротация. Не хранить секреты в репозитории или образе.
- TLS на внешнем ingress; для service-to-service — явная политика TLS/mTLS, доверенных сертификатов и сетевых ACL. NATS — TLS и авторизация subject. Не принимать идентичность из неподписанных заголовков gateway.
- Ядро предоставляет точки подключения auth middleware/interceptors, но не реализует бизнес-авторизацию или универсальный JWT policy. AuthService/потребители владеют проверкой прав.
- Ограниченные request sizes, deadlines, concurrency и queues. Recovery на transport boundary не скрывает неожиданные ошибки; клиент получает безопасное сообщение, журнал — достаточные данные без секретов.
- Контейнер non-root, минимальные разрешения, read-only FS при возможности; health/metrics не доступны из публичной сети.
- Не включать небезопасные development defaults незаметно в production. При явно выбранном production-профиле проверять обязательные security-настройки подключённых модулей.

Источники: [pgxpool](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool), [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/), [Prometheus Go client](https://github.com/prometheus/client_golang).
