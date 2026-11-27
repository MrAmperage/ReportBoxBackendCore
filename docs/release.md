# Проверка и выпуск

[Архитектура](architecture.md) · [Текущее состояние](development.md)

## Release gate

Выполнять для каждого выпуска на чистом checkout кандидата. Использовать Go не старее указанной версии в `go.mod` и актуальный поддерживаемый security patch. Источник точных версий зависимостей — `go.mod` и `go.sum`, а не ADR. Не менять опубликованные теги. Наличие тестов не означает, что они прошли.

```bash
go version
go mod tidy -diff
go mod verify
test -z "$(gofmt -l .)"
go vet ./...
go test -race -count=1 ./...
go build ./...
```

При изменении зависимостей использовать реальный `go get` и `go mod tidy`, проверить diff и `go mod verify`; не писать checksums вручную и не отключать GOSUMDB. Не выполнять обновление всех зависимостей автоматически при каждом релизе. Проверить достижимые уязвимости через [govulncheck](https://go.dev/security/vuln/): установить совместимую актуальную версию `golang.org/x/vuln/cmd/govulncheck` с явным тегом, выполнить `govulncheck ./...`. Рассмотреть обнаруженные проблемы; не подавлять ошибки сканера. Версии зависимостей сами по себе не доказывают отсутствие уязвимостей.

## Интеграция

`TestReleaseGraph` проверяет полный граф Fx с Telemetry и без неё, пассивность до Start. `TestReleaseIntegration` проверяет Connect/gRPC/gRPC-Web, GORM/pgx, Core NATS, JetStream, Management, Prometheus/OTLP и освобождение ресурсов. `TestReleaseRollback` проверяет освобождение ресурсов при конфликте порта. Static имеет собственные lifecycle и HTTP тесты, а также тесты traversal, symlink, Host guard, overload и shutdown. Для полного прогона нужны оба выделенных тестовых сервера одновременно:

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
go test -race -count=3 ./static
go test -race -count=3 ./auth/...
docker rm -f reportbox-nats-test reportbox-postgres-test
```

Auth проверяет signatures, strict parsing, provider failure/timeout, Fx, RPC policies и реальный listener; не требует NATS/PostgreSQL для собственных тестов. Проверять также реализацию freshness и ротацию ключей конкретного provider в сервисе-потребителе. Без обоих ENV совместные тесты пропускаются. Не использовать production credentials или ресурсы. Дополнительно проверить browser Connect, gRPC TLS/mTLS, ограничения публичной маршрутизации, закрытость Management, реальный frontend (SPA, chunks, cache), SIGTERM/rollback, reconnect/duplicates, отключённый collector. Проверить нагрузку на RPC, пул, очереди и Static с контролируемыми лимитами; pprof только локально, не через публичный endpoint. Остановка требует cooperative context; единичный smoke не гарантирует производительность.

## Автоматизация

CI должен проверять форматирование, `go mod tidy -diff`, verify, vet, race, build, интеграцию с обоими тестовыми серверами и govulncheck. Не считать CI выполненным при отсутствии workflow или результата. Успешные результаты обязательных проверок фиксируются в [текущем состоянии](development.md), без накопительного журнала. При недоступности проверки явно указать причину и не выдавать её за успешную.

## Публикация

После подтверждения release gate владельцем слить проверенные изменения в `main`, проверить финальный коммит, повторить необходимые проверки. Создать аннотированный тег только вручную по явному решению владельца (пример команды для выбранной версии):

```bash
git switch main
git pull --ff-only
git status
# VERSION — согласованный новый SemVer-тег
read -r VERSION
git tag -a "$VERSION" -m "ReportBoxBackendCore $VERSION"
git push origin "$VERSION"
```

Не перемещать существующий тег; при исправлении выпускать новую версию. GitHub Release необязателен для Go module. Потребители закрепляют опубликованный тег; изменения публичного API описываются явно, для несовместимых изменений учитывать SemVer. Библиотека не выпускает обязательный Docker-образ. После публикации актуализировать `docs/development.md` без изменения уже опубликованного тега.
