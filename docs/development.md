# Разработка, проверка и выпуск

[К оглавлению](../README.md)

## 1. Порядок реализации

| Этап | Объём | Критерий готовности |
|---|---|---|
| 0 | Спецификация в Documentation | Архитектурные решения рассмотрены, открытые вопросы отмечены |
| 1 | app/config/logging/lifecycle/health, корневой API | Минимальный процесс стартует, останавливается по сигналу, валидирует config; тесты rollback и timeout |
| 2 | HTTP, management, frontend | Работают health endpoints и remote assets; тесты HTTP security, cache, bind failure и остановки |
| 3 | gRPC server/client | Пример двух сервисов, deadlines, mapping ошибок, health и shutdown |
| 4 | NATS + JetStream | Публикация, durable consumer, redelivery, обработка reconnect и drain, проверка duplicate |
| 5 | PostgreSQL + telemetry | Пул, readiness, метрики, trace через HTTP/gRPC/event; управляемое закрытие |
| 6 | Examples, CI, документация и release | Пример сервиса-потребителя, интеграционные тесты, воспроизводимая сборка и первый тег v0.x |

Не создавать все пакеты и пустые заглушки заранее. После каждого этапа актуализировать README и документацию реализованного API. При изменении согласованного решения записывать короткий ADR с мотивацией. Ветка `Documentation` предназначена для спецификации; код разрабатывается после её утверждения. Стратегия остальных веток и первый номер релиза уточняются отдельно.

## 2. Тестирование

Unit tests: table-driven config/defaults/validation, логирование и redaction, health transitions, subject/cache policy и ошибки. Использовать обычные Go constructors и подменяемые зависимости, `httptest`, `fxtest`/`fx.ValidateApp`. Не заменять интеграцию только моками. Проверять отсутствие гонок и утечек ресурсов.

Integration: тестовые NATS с JetStream и PostgreSQL в контейнерах, gRPC loopback/bufconn, реальный HTTP listener для проверки graceful shutdown. Использовать dynamic ports, уникальные namespaces и временные каталоги. Тестовый env не зависит от production. Предусмотреть повторяемый сценарий `docker compose` или Testcontainers, выбор инструмента ещё не утверждён.

Обязательные негативные сценарии:

- Некорректный config, обязательная зависимость недоступна, конфликт порта, ошибка частичного OnStart и rollback.
- SIGTERM при HTTP/gRPC запросе и при обработке JetStream сообщения; readiness снимается до закрытия зависимостей; timeout приводит к завершению, без зависших goroutines.
- Потеря NATS/DB, recovery, duplicate events и ошибочный consumer, исчерпание retry; liveness остаётся независимым.
- Frontend: неверный prefix, пропавший chunk, traversal, symlink, неподдерживаемый метод, отсутствие directory listing, conditional GET, кеш entry-файлов и CORS.
- Никакие secrets из конфигурации и заголовков не попадают в error response или log.

## 3. CI и качество

Предлагаемые проверки pull request и основной ветки после начала реализации:

```bash
go fmt ./...
# CI должен проверять, а не коммитить изменения форматирования.
go vet ./...
go test ./...
go test -race ./...
go build ./...
go mod verify
```

Дополнительно: `govulncheck ./...`, проверка незакоммиченных изменений после `go mod tidy`, отдельный integration job с сервис-контейнерами и ограничением времени. В CI форматирование проверяется через `gofmt -l` и fail при непустом выводе. Pin версий actions/dependencies; минимальные permissions. Версия Go определяется при инициализации `go.mod` на основании поддерживаемой stable ветки и совместимости зависимостей, а не автоматически на каждом CI запуске.

## 4. Публичный API и документация

Идиоматичное Go naming, ошибки с контекстом (`%w` где уместно), context первым аргументом, документирование exported types. Один Go module; зависимости фиксируются в `go.sum`. SemVer: до v1 API может меняться с явным описанием миграции; после v1 breaking changes требуют major version. Потребители закрепляют конкретный тег, не используют плавающий main в production. Библиотека не публикует Docker-образ как обязательную часть релиза — образ строит сервис-потребитель.

Examples: минимальный core, HTTP+embedded frontend, worker с NATS, пример gRPC; примеры должны компилироваться и проверяться CI. Для первого полноценного smoke test взять простой сервис без доменных зависимостей; AuthService интегрируется после стабилизации нужных модулей.

Definition of Done модуля: работает без ненужных подключений; явно определены config, lifecycle, defaults, ошибки и security; unit/integration tests соответствуют рискам; пример использования; обновлена документация; нет очевидных гонок, утечек, секретов и ненужных абстракций.
