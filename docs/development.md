# Разработка, проверка и выпуск

[К описанию проекта](../README.md)

## 1. Порядок реализации

| Этап | Объём | Критерий готовности |
|---|---|---|
| 0 | Спецификация в Documentation | Архитектурные решения рассмотрены, открытые вопросы отмечены |
| 1 | app/config/logging/lifecycle/health, корневой API | Минимальный процесс стартует, останавливается по сигналу, валидирует config; тесты rollback и timeout |
| 2 | RPC (ConnectRPC server/client) | Go-to-Go и browser через публичный origin, HTTP/2 interoperability, deadlines, auth hooks, drain |
| 3 | NATS + JetStream | Публикация, durable consumer, redelivery, reconnect, drain и duplicate |
| 4 | PostgreSQL + GORM | Общий пул, CRUD, транзакции, readiness, управляемое закрытие |
| 5 | Management | Закрытые health endpoints, bind failure, shutdown |
| 6 | Telemetry | Метрики, trace через RPC/event, управляемый flush |
| 7 | Examples, интеграционный CI, документация и release | Первый тег v0.1.0 |
| 8 | Static и независимая раздача UI | Отдельный listener, безопасный root, SPA, кандидат v0.1.1 |

Минимальный пример запуска и базовый CI (format/vet/test/build) требуются с этапа 1; тесты и интеграционные проверки наращивать одновременно с модулями. Не создавать все пакеты и пустые заглушки заранее. После каждого этапа актуализировать README и документацию реализованного API. При изменении согласованного решения записывать короткий ADR с мотивацией. Ветка `Documentation` предназначена для спецификации; код разрабатывается после её утверждения. Ветка `Release` использовалась для стабилизации перед тегом `v0.1.0`; публикация остаётся за владельцем.

Этап 1–2: Core и RPC присутствуют в `main` (`b077f03`). Содержимое main и RPC на момент перехода совпало (44 файла). Владелец сообщил о локальной проверке и обновлении main; конкретные логи тестов не предоставлены. CI workflow отсутствовал в main, запусков Actions не найдено. Реальные browser/ingress и TLS/mTLS сценарии отдельно не подтверждены. См. [Core](core.md), [RPC](rpc.md), [ADR 0004](adr/rpc.md).

Этап 3: NATS и JetStream перенесены в `main` (`6dacff9`). Проверено совпадение всех 59 файлов с веткой `NATS` (`6c1f54a`). Владелец сообщил о переносе, но конкретные результаты полной сборки и интеграционных тестов не предоставлены. CI отсутствует в main. См. [NATS](nats.md) и [ADR 0005](adr/nats.md); сохраняются указанные там ограничения reconnect/Fetch и ACL.

Этап 4: PostgreSQL + GORM перенесены в `main` (`e9d1a4b`), проверено совпадение всех 75 файлов с веткой `PostgreSQL` (`4273d06`). Сохранена общая архитектура pgxpool/GORM. Ревизия конфигурации, TLS, startup cleanup и транзакций опубликована; полная сборка и интеграционные тесты независимо не подтверждены. См. [PostgreSQL](postgres.md) и [ADR 0006](adr/postgres.md).

Этап 5: Management перенесён в `main` (`dd6dd0a`); проверено совпадение всех 87 файлов с `Management` (`3cdc51a`). Реализация и ревизия опубликованы. CI workflow отсутствует; полная сборка и интеграционные тесты независимо не подтверждены. См. [Management](management.md) и [ADR 0007](adr/management.md).

Этап 6: Telemetry перенесена в `main` (`6ad84c7`); проверено совпадение всех 106 файлов с веткой `Telemetry` (`3a676fb`). Владелец подтвердил локальную проверку перед merge, логи не предоставлены. Ревизия кардинальности, stream lifecycle, trace propagation и OTLP shutdown опубликована. См. [Telemetry](telemetry.md), [ADR 0008](adr/telemetry.md). CI workflow отсутствует в опубликованном main.

Этап 7: `v0.1.0` опубликован на main `1ffd150fdcab01d96b34de0cf0a69db875e39c07`. Проверено совпадение дерева тега и main (110 файлов). Владелец сообщил о локальной проверке и обновлении `go.mod`/`go.sum`: Go 1.27.4, OTel 1.45.0, gRPC 1.85.0, x/net 0.62.0. CI отсутствует, независимых результатов полного integration, govulncheck и нагрузки нет. См. [ADR 0009](adr/release.md).

Этап 8: подготовлен кандидат `v0.1.1` от `1ffd150`. По решению владельца проект больше не привязан к конкретной внешней маршрутизации. Добавлен опциональный `static.Module()` (каталог готовой сборки, отдельный HTTP listener, opt-in SPA, root confinement, таймауты и лимит активных handlers). Существующие модули и версии зависимостей не меняются; изменение RPC ограничено комментарием. Документация актуализирована. См. [Static](static.md), [ADR 0010](adr/static.md). Кандидат передаётся архивом изменённых файлов, не опубликован.

Проверки кандидата: `gofmt`, `GO111MODULE=off go test -race -count=3 static/config.go static/handler.go static/config_test.go static/handler_test.go` (три повторения) и `go vet` этих файлов на Go 1.23.2 успешны. Проверены path traversal, скрытые файлы/source maps, HTTP методы, SPA и cache/MIME/Range. Полный пакет Static (Fx, реальный listener и os.Root confinement) и сборка остальных пакетов не запущены: попытка загрузить Go 1.27.4 завершилась DNS connection refused к `proxy.golang.org`. CI workflow добавлен к архиву, но запусков ещё нет.

**Следующий шаг:** применить архив в отдельной ветке (например `Static`), выполнить тесты Go 1.27.4+, CI, browser smoke и проверки безопасности. После успешной проверки слить в main и создать тег `v0.1.1`. Предыдущий тег не изменять. См. [release gate](release.md).

## 2. Тестирование

Unit tests: table-driven config/defaults/validation, логирование и redaction, health transitions, subject/cache policy и ошибки. Использовать обычные Go constructors и подменяемые зависимости, `httptest`, `fxtest`/`fx.ValidateApp`. Не заменять интеграцию только моками. Проверять отсутствие гонок и утечек ресурсов.

Integration: тестовые NATS с JetStream и PostgreSQL в контейнерах, ConnectRPC HTTP/1.1 и HTTP/2 loopback, native gRPC interoperability и внешний reverse proxy, реальный HTTP listener для проверки graceful shutdown. Использовать dynamic ports, уникальные namespaces и временные каталоги. Тестовый env не зависит от production. Нужны отдельные subprocess tests для `Run`, SIGTERM и exit code; прямой `fx.App.Start/Stop` не проверяет поведение ОС/CLI. Проверять порядок фаз координатора при перестановке модулей, пропущенной зависимости, цикле и ошибке частичного старта. Предусмотреть повторяемый сценарий `docker compose` или Testcontainers, выбор инструмента ещё не утверждён.

Обязательные негативные сценарии:

- Некорректный config, обязательная зависимость недоступна, конфликт порта, ошибка частичного OnStart и rollback.
- SIGTERM при RPC/management запросе и при обработке JetStream сообщения; readiness снимается до закрытия зависимостей; timeout приводит к контролируемой ошибке/forced close; проверять отсутствие зависших кооперативных goroutines.
- Потеря NATS/DB, recovery, duplicate events и ошибочный consumer, исчерпание retry; liveness остаётся независимым.
- RPC: запрещённые публичные процедуры, корректный path, h2c, ограничения размера, deadline, stream cancellation и ошибки proxy. UI deployment отдельно: prefix, пропавший chunk, traversal, symlink, directory listing, кеш, rollback и CORS; изоляция внешней маршрутизации.
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

Examples: минимальный core, ConnectRPC server/client и Angular через публичный origin, worker с NATS, management; примеры должны компилироваться и проверяться CI. Для первого полноценного smoke test взять простой сервис без доменных зависимостей; AuthService интегрируется после стабилизации нужных модулей.

Definition of Done модуля: работает без ненужных подключений; явно определены config, lifecycle, defaults, ошибки и security; unit/integration tests соответствуют рискам; пример использования; обновлена документация; нет очевидных гонок, утечек, секретов и ненужных абстракций.
