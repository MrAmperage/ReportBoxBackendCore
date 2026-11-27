# Хранилище, наблюдаемость и безопасность

[К описанию проекта](../README.md)

## 1. PostgreSQL

Согласованный подход: `postgres.Module()` управляет общим `pgxpool.Pool`; `postgres/orm.Module()` подключает GORM поверх него. GORM Generics API предпочтителен для прикладных репозиториев. Нативный pgx доступен для специальных операций (COPY, сложные запросы). См. [API PostgreSQL](postgres.md) и [ADR 0006](adr/postgres.md).

Миграции принадлежат сервису, выполняются отдельным deployment job/CLI с контролируемым lock, не каждым экземпляром при старте. Ядро не создаёт прикладные таблицы и не запускает AutoMigrate. Транзакции и isolation выбираются приложением. Использовать context, параметризованные условия, корректное закрытие rows/rollback. Не читать чужую БД напрямую вместо API/event-контракта. Общий пул не означает общую транзакцию между pgx и GORM.

Readiness проверяет обязательную БД с коротким deadline; liveness от БД не зависит. Закрытие ORM-адаптера и затем pgxpool происходит после drain обработчиков. `pgxpool.Close()` не принимает context; при нарушении deadline модуль возвращает ошибку, но не гарантирует завершение неосвобождённых ресурсов. Политика TLS и конфигурация описаны в профильном документе. DSN и SQL с параметрами не логируются.

## 2. Observability

`telemetry.Module()` опционален: отдельный Prometheus registry, process/Go runtime collectors, OpenTelemetry SDK и OTLP HTTP/protobuf export при заданном endpoint. Management принимает `management.MetricsHandler`; без provider `/metrics` возвращает 404. Подключение collector не является условием readiness/liveness. Провайдер и exporter создаются при Open; queue ограничена, shutdown пытается завершить экспорт в lifecycle deadline. Без модуля транспорт работает без активных метрик и SDK.

Встроенные RPC unary/streaming, Core NATS receive, JetStream process/ack/nak и GORM metrics; pgxpool и NATS статистика снимаются локально, без сетевых запросов при scrape. `slog` добавляет Trace ID/Span ID из context. Native publishers передают W3C trace context явно через `Runtime.Propagator()`. Не переносить credentials через baggage, SQL, headers, query values или payload в телеметрию. Не использовать UUID, message IDs, динамические subjects и тексты ошибок как labels.

Конфигурация, фактические имена метрик, lifecycle, ограничения и примеры описаны в [Telemetry](telemetry.md) и [ADR 0008](adr/telemetry.md).

## 3. Безопасность по умолчанию

- Least privilege: отдельные учётные данные PostgreSQL и NATS для сервисов, ограниченные grants/subjects, ротация. Не хранить секреты в репозитории или образе.
- TLS на внешнем ingress; только разрешённые публичные RPC маршрутизируются наружу; для service-to-service — явная политика TLS/mTLS, доверенных сертификатов и сетевых ACL. NATS — TLS и авторизация subject. Не принимать идентичность из неподписанных заголовков gateway.
- Ядро предоставляет точки подключения auth middleware/interceptors, но не реализует бизнес-авторизацию или универсальный JWT policy. AuthService/потребители владеют проверкой прав.
- Ограниченные request sizes, deadlines, concurrency и queues. Recovery на transport boundary не скрывает неожиданные ошибки; клиент получает безопасное сообщение, журнал — достаточные данные без секретов.
- Контейнер non-root, минимальные разрешения, read-only FS при возможности; health/metrics не доступны из публичной сети.
- Не включать небезопасные development defaults незаметно в production. При явно выбранном production-профиле проверять обязательные security-настройки подключённых модулей.

Источники: [pgxpool](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool), [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/), [Prometheus Go client](https://github.com/prometheus/client_golang).
