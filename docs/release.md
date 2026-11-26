# Подготовка первого релиза v0.1.0

[Архитектура](architecture.md) · [План и состояние](development.md) · [ADR 0009](adr/release.md)

## 1. Release gate

Текущее состояние — кандидат изменений для ветки `Release` от main `6ad84c7`, не выпущенная версия. Публичные API пока не считаются стабильными v1. Все инфраструктурные модули реализованы; локальную проверку предыдущей версии владелец подтвердил. В этом окружении невозможно подтвердить полную сборку кандидата.

- [ ] Обновлены и проверены версии зависимостей, `go.mod` и `go.sum` сгенерированы Go.
- [ ] `gofmt`, `go mod tidy -diff`, `go mod verify`, `go vet`, `go test -race`, `go build` успешны на актуальном патче Go 1.27.
- [ ] `govulncheck` не находит достижимых уязвимостей; результаты для недостижимых функций рассмотрены. Не подавлять ошибки сканера автоматически.
- [ ] `TestReleaseGraph`, `TestReleaseIntegration`, `TestReleaseRollback` и существующие unit/integration tests успешны с обоими тестовыми серверами.
- [ ] Проверены Caddy/browser Connect, gRPC с TLS/mTLS и закрытость Management; Caddy/NaiveProxy не изменять без отдельного согласования.
- [ ] Выполнен smoke с отключённым collector; контролируемый нагрузочный тест RPC, DB pool и очередей; при необходимости pprof — только в локальной тестовой среде, не через публичный endpoint. Зафиксировать результаты; не выдавать единичный smoke за гарантию производительности.
- [ ] CI workflow присутствует в GitHub, оба jobs зелёные на релизном коммите.
- [ ] Документация соответствует проверенному API; владелец подтвердил merge и тег.

## 2. Зависимости и безопасность

Ручная сверка Go Vulnerability Database выявила следующие версии, входящие в затронутые диапазоны на базе `6ad84c7` (26 ноября 2026). Это не полный scan call graph. Некоторые уязвимости относятся к неиспользуемым подсистемам; ConnectRPC не использует grpc-go как свой основной transport. Не считать пакет уязвимым в конкретном сервисе только по версии, но не игнорировать исправления перед выпуском.

| Advisory | Текущая версия | Решение-кандидат |
|---|---|---|
| [GO-2026-4394](https://pkg.go.dev/vuln/GO-2026-4394), OTel SDK resource (в списке есть `NewSchemaless`) | v1.38.0 | OTel API/SDK/trace/otlptrace v1.45.0 или выше |
| [GO-2026-6505](https://pkg.go.dev/vuln/GO-2026-6505), exporter config logs (unreviewed) | v1.38.0 | та же согласованная группа OTel версий |
| [GO-2026-5506](https://pkg.go.dev/vuln/GO-2026-5506), OTel baggage; приложение не использует baggage propagation | v1.38.0 | та же группа OTel версий |
| [GO-2026-6348](https://pkg.go.dev/vuln/GO-2026-6348), [GO-2026-6443](https://pkg.go.dev/vuln/GO-2026-6443), grpc transport/xDS | v1.75.0 (indirect) | grpc v1.85.0 или выше |
| [GO-2026-6860](https://pkg.go.dev/vuln/GO-2026-6860), [GO-2026-6866](https://pkg.go.dev/vuln/GO-2026-6866), x/net/html | v0.51.0 | x/net v0.62.0 или выше |
| [GO-2026-6354](https://pkg.go.dev/vuln/GO-2026-6354), [GO-2026-6355](https://pkg.go.dev/vuln/GO-2026-6355), x/crypto/ssh | v0.49.0 | x/net v0.62.0 требует x/crypto v0.58.0; проверить MVS |

[Go release history](https://go.dev/doc/devel/release) / [Go VERSION](https://go.dev/VERSION): актуальный патч на дату ревизии — Go 1.27.4. Системный Go должен быть не старее 1.27.4. Указанные версии — проверенные опубликованные кандидаты, **не подтверждённый исправленный граф**. upstream `go.mod` OTel v1.45.0, grpc v1.85.0, x/net v0.62.0 проверены; API `otlptrace.Client` сохранён. Не требуется отдельный RPC стек.

Из корня чистого checkout ветки Release после применения архива:

```bash
bash scripts/prepare-release.sh
# Проверь изменения, не публикуй неожиданные зависимости.
git diff -- go.mod go.sum
go install golang.org/x/vuln/cmd/govulncheck@v1.10.0
"$(go env GOPATH)/bin/govulncheck" ./...
```

Сценарий однократной подготовки (не запускается автоматически при старте сервисов и в CI): Go обновляет module graph, вычисляет реальные суммы, выполняет tidy/verify. `go.mod`/`go.sum` из текущей ветки сохранены в исходном виде до его запуска. Если `go get`/scan обнаружит несовместимость или новые advisory, устранить до merge; не отключать `GOSUMDB`. Указанные версии не являются обещанием отсутствия иных уязвимостей.

## 3. Общая интеграция

`TestReleaseGraph` собирает полный Fx graph с Telemetry и без неё в намеренно непоследовательном порядке модулей; проверяет, что соединения и listeners не открываются до Start. `TestReleaseIntegration` использует реальные NATS + JetStream и PostgreSQL одновременно, вызывает RPC по Connect, gRPC h2c, gRPC-Web, выполняет SELECT через GORM, публикует события и jobs нативным NATS API, проверяет получение и ack, endpoints, метрики и совпадение Trace ID в обработчиках, реальные OTLP protobuf spans и закрытие ресурсов. Без Telemetry проверяет 404 `/metrics`, отсутствие tracing и нормальную прикладную работу. `TestReleaseRollback` резервирует RPC TCP port и проверяет rollback ранее открытых ресурсов после bind failure. Не требует production БД, бизнес-таблиц, Caddy или внешнего collector.

Все тесты с внешними зависимостями используют только выделенные тестовые серверы. CI включает оба ENV одновременно. Локальный сценарий:

```bash
docker run --rm -d --name reportbox-nats-test -p 127.0.0.1:4222:4222 nats:2.11-alpine -js
docker run --rm -d --name reportbox-postgres-test \
  -e POSTGRES_USER=reportbox -e POSTGRES_PASSWORD=localtest \
  -e POSTGRES_DB=reportbox -p 127.0.0.1:55432:5432 postgres:17-alpine
# Дождаться готовности обоих серверов.
docker exec reportbox-postgres-test pg_isready -U reportbox -d reportbox
export NATS_TEST_URL=nats://127.0.0.1:4222
export TEST_POSTGRES_DSN='postgres://reportbox:localtest@127.0.0.1:55432/reportbox?sslmode=disable'
go test -race -count=1 ./...
docker rm -f reportbox-nats-test reportbox-postgres-test
```

Без обоих ENV совместные интеграционные тесты пропускаются, а не подтверждают интеграцию. Изолированные тесты Go 1.23 не заменяют проверку на Go 1.27. Порядок остановки гарантирован для участников ядра, не для произвольного кода, игнорирующего context. Неизбежные ограничения и обязательства прикладных сервисов описаны в [Core](core.md), [RPC](rpc.md), [NATS](nats.md), [PostgreSQL](postgres.md), [Management](management.md), [Telemetry](telemetry.md).

## 4. Публикация

Загрузить `.github/workflows/go.yml` (скрытая папка), дождаться двух зелёных jobs `checks` и `security` на Release. Устранить отклонения. После подтверждения владельцем merge в main, повторить CI, убедиться в отсутствии чужих изменений, затем создать тег вручную на проверенном main:

```bash
git switch main
git pull --ff-only
git tag -a v0.1.0 -m "BackendCore v0.1.0"
git push origin v0.1.0
```

Эти команды выполняются владельцем только после release gate. Библиотека не публикует Docker-образ, не выпускает прикладные микросервисы. Потребитель закрепляет `github.com/MrAmperage/ReportBoxBackendCore@v0.1.0`. Следующий прикладной проект — AuthService; его схемы, миграции, токены, авторизация, Protobuf contracts и UI остаются вне Core.
