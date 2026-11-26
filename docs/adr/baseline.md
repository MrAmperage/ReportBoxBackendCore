# ADR 0001 — Базовый каркас ReportBox

[К оглавлению](../../README.md)

**Статус:** согласованный общий стек; детали ниже предложены для review. Спецификация 0.1, 26 ноября 2026 г.

## Согласовано в обсуждении

- Репозиторий `MrAmperage/ReportBoxBackendCore`, разработка на Go. Документация первоначально в ветке `Documentation`, публикация пользователем.
- Uber Fx как DI/lifecycle вместо тяжёлого service framework. Модульный подход с опциональными инфраструктурными компонентами.
- gRPC + Protobuf для синхронных внутренних RPC; NATS + JetStream для событий и jobs; HTTP для внешнего API и технических endpoints.
- pgx, slog, OpenTelemetry, Prometheus. Без Watermill, Kafka и RabbitMQ на старте.
- Сервис с UI умеет раздавать собственный microfrontend; ядро поддерживает embedded FS и каталог. Backend и UI могут выпускаться одним артефактом. Отдельный management listener.

## Предложено в спецификации (требует утверждения)

| Решение | Почему |
|---|---|
| Один Go module, package `backendcore`, `New`/`Run` | Простая композиция и тестируемый lifecycle |
| Отдельные пакеты `httpserver`, `management`, `grpcserver`, `grpcclient`, `jetstream` | Избежать коллизий имён и не включать лишние ресурсы |
| Core включает health registry, но не открывает порты | Подходит даже фоновой задаче |
| Env-first, typed config каждого модуля | Меньше зависимостей, явная валидация |
| Один публичный origin через gateway, frontend на `/frontend/` | Упрощает CORS и развертывание |
| Default ports 8080/8081/9090, start/stop timeout 30s | Понятная исходная конфигурация; всё изменяемо |
| Durable pull consumers, явный ack, idempotency | Управляемая повторная доставка |
| Subject namespace и Protobuf envelope из messaging.md | Версионирование и correlation; окончательный формат ещё открыт |

## Открытые вопросы до соответствующего этапа

| Вопрос | Когда решить |
|---|---|
| Версия Go, публичные сигнатуры и точные версии зависимостей | Этап 1 |
| Конфигурация KrakenD/reverse proxy, Native Federation или Module Federation, shared Angular dependencies, rollout старых assets | Этап 2 |
| Схема service discovery, место публикации общих Protobuf contracts, TLS/mTLS и service identity | Этап 3 |
| Envelope vs payload+headers, stream/consumer provisioning, retention, DLQ, ownership outbox/inbox | Этап 4 |
| Migration tooling, политика PostgreSQL TLS, OTLP collector и метрики | Этап 5 |
| Стратегия веток после Documentation, release tags, лицензия публичного проекта | До первого выпуска |

## Альтернативы

Kratos и Go Micro могли бы дать более широкий service framework, но вводят собственные абстракции и не нужны при выбранном наборе транспортов. Go-kit требует самостоятельной сборки значительной части инфраструктуры. Fx выбран за независимость от транспорта; цена — необходимость правильно проектировать lifecycle и DI graph. Watermill может быть рассмотрен при росте потребности в event middleware, но на старте нативный NATS API достаточно прозрачен. Раздача UI внешним CDN/proxy может понадобиться для масштабирования; поддержка fs.FS не запрещает этот вариант.

## Последствия

Сервисы запускаются и тестируются самостоятельно; ядро растёт небольшими проверяемыми модулями. Прямой выбор библиотек требует контроля совместимости и тестов на негативные сценарии. Ни один из проектов API пока не является реализованным контрактом. Не использовать спецификацию как утверждение о готовности production.
