# Разработка, проверка и выпуск

[К описанию проекта](../README.md)

## 1. Порядок реализации

| Этап | Объём | Критерий готовности |
|---|---|---|
| 0 | Спецификация в Documentation | Архитектурные решения рассмотрены, открытые вопросы отмечены |
| 1 | app/config/logging/lifecycle/health, корневой API | Минимальный процесс стартует, останавливается по сигналу, валидирует config; тесты rollback и timeout |
| 2 | RPC (ConnectRPC server/client) | Go-to-Go и browser через Caddy, HTTP/2 interoperability, deadlines, auth hooks, drain |
| 3 | NATS + JetStream | Публикация, durable consumer, redelivery, reconnect, drain и duplicate |
| 4 | PostgreSQL | Пул, readiness, управляемое закрытие |
| 5 | Management | Закрытые health endpoints, bind failure, shutdown |
| 6 | Telemetry | Метрики, trace через RPC/event, управляемый flush |
| 7 | Examples, интеграционный CI, документация и release | Воспроизводимая сборка, примеры потребителей, первый тег v0.x |

Минимальный пример запуска и базовый CI (format/vet/test/build) требуются с этапа 1; тесты и интеграционные проверки наращивать одновременно с модулями. Не создавать все пакеты и пустые заглушки заранее. После каждого этапа актуализировать README и документацию реализованного API. При изменении согласованного решения записывать короткий ADR с мотивацией. Ветка `Documentation` предназначена для спецификации; код разрабатывается после её утверждения. Стратегия остальных веток и первый номер релиза уточняются отдельно.

Этап 1–2: Core и RPC присутствуют в `main` (`b077f03`). Содержимое main и RPC на момент перехода совпало (44 файла). Владелец сообщил о локальной проверке и обновлении main; конкретные логи тестов не предоставлены. CI workflow отсутствовал в main, запусков Actions не найдено. Реальные Caddy/browser и TLS/mTLS сценарии отдельно не подтверждены. См. [Core](core.md), [RPC](rpc.md), [ADR 0004](adr/rpc.md).

Этап 3: ветка `NATS` создана от `b077f03`. Подготовлен кандидат NATS + JetStream, см. [API](nats.md) и [ADR 0005](adr/nats.md), с unit и opt-in broker integration tests (Core delivery, JetStream redelivery, duplicate publish, ACK during shutdown). Публикация владельцем и полная сборка не подтверждены. Локально прошли только независимые проверки config с race и vet; форматирование новых Go-файлов проверено; полный `go mod tidy` остановился на недоступном Go proxy при скачивании Go 1.27.0. Docker и broker отсутствуют. Workflow для NATS добавлен в архив, но ещё не опубликован.

**Следующий шаг:** распаковать изменения в ветку NATS, выполнить `go mod tidy`, `go mod verify`, format/vet/test/race/build, запустить интеграционные тесты с изолированным broker (см. [пример](../examples/messaging-demo/README.md)). Провести review lifecycle, TLS/ACL, reconnect и drain активных обработчиков; дополнить тесты этих сценариев до принятия. После проверки и слияния перейти к PostgreSQL. Передавать актуальное состояние в документации согласно [AGENTS.md](../AGENTS.md#8-преемственность-между-диалогами).

UI публикуется отдельно через Caddy, вне этапов Go-модулей. См. [ADR 0003](adr/edge.md) и [HTTP/Frontend](http-frontend.md).

## 2. Тестирование

Unit tests: table-driven config/defaults/validation, логирование и redaction, health transitions, subject/cache policy и ошибки. Использовать обычные Go constructors и подменяемые зависимости, `httptest`, `fxtest`/`fx.ValidateApp`. Не заменять интеграцию только моками. Проверять отсутствие гонок и утечек ресурсов.

Integration: тестовые NATS с JetStream и PostgreSQL в контейнерах, ConnectRPC HTTP/1.1 и HTTP/2 loopback, native gRPC interoperability и Caddy reverse proxy, реальный HTTP listener для проверки graceful shutdown. Использовать dynamic ports, уникальные namespaces и временные каталоги. Тестовый env не зависит от production. Нужны отдельные subprocess tests для `Run`, SIGTERM и exit code; прямой `fx.App.Start/Stop` не проверяет поведение ОС/CLI. Проверять порядок фаз координатора при перестановке модулей, пропущенной зависимости, цикле и ошибке частичного старта. Предусмотреть повторяемый сценарий `docker compose` или Testcontainers, выбор инструмента ещё не утверждён.

Обязательные негативные сценарии:

- Некорректный config, обязательная зависимость недоступна, конфликт порта, ошибка частичного OnStart и rollback.
- SIGTERM при RPC/management запросе и при обработке JetStream сообщения; readiness снимается до закрытия зависимостей; timeout приводит к контролируемой ошибке/forced close; проверять отсутствие зависших кооперативных goroutines.
- Потеря NATS/DB, recovery, duplicate events и ошибочный consumer, исчерпание retry; liveness остаётся независимым.
- RPC: запрещённые публичные процедуры, корректный path, h2c, ограничения размера, deadline, stream cancellation и ошибки proxy. UI deployment отдельно: prefix, пропавший chunk, traversal, symlink, directory listing, кеш, rollback и CORS; smoke test NaiveProxy после изменения Caddy.
- Никакие secrets из конфигурации и заголовков не попадают в error response или log. Проверить неверный DSN/URL, необработанные driver errors, readiness и trace attributes.
- Отсутствие открытых ресурсов при ошибке построения DI graph; проверка lazy-конструирования ресурса, отменённых startup/stop contexts и позднего результата health check после shutdown.
- Отдельный smoke test core без сети, management без RPC listener, RPC client без server; отсутствие обязательного telemetry SDK для транспортов.

## 3. CI и качество

Предлагаемые проверки pull request и основной ветки после начала реализации:

```bash
test -z "$(gofmt -l .)"
# CI проверяет форматирование, а не исправляет файлы.
go vet ./...
go test ./...
go test -race ./...
go build ./...
go mod verify
```

Дополнительно: `govulncheck ./...`, проверка незакоммиченных изменений после `go mod tidy`, отдельный integration job с сервис-контейнерами и ограничением времени. В CI форматирование проверяется через `gofmt -l` и fail при непустом выводе. Pin версий actions/dependencies; минимальные permissions. Версия Go определяется при инициализации `go.mod` на основании поддерживаемой stable ветки и совместимости зависимостей, а не автоматически на каждом CI запуске.

## 4. Публичный API и документация

Идиоматичное Go naming, ошибки с контекстом (`%w` где уместно), context первым аргументом, документирование exported types. Один Go module; зависимости фиксируются в `go.sum`. SemVer: до v1 API может меняться с явным описанием миграции; после v1 breaking changes требуют major version. Потребители закрепляют конкретный тег, не используют плавающий main в production. Библиотека не публикует Docker-образ как обязательную часть релиза — образ строит сервис-потребитель.

Examples: минимальный core, ConnectRPC server/client и Angular через Caddy, worker с NATS, management; примеры должны компилироваться и проверяться CI. Для первого полноценного smoke test взять простой сервис без доменных зависимостей; AuthService интегрируется после стабилизации нужных модулей.

Definition of Done модуля: работает без ненужных подключений; явно определены config, lifecycle, defaults, ошибки и security; unit/integration tests соответствуют рискам; пример использования; обновлена документация; нет очевидных гонок, утечек, секретов и ненужных абстракций.
