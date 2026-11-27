# ReportBoxBackendCore

Базовая Go-библиотека для микросервисов ReportBox: Fx, ConnectRPC, NATS/JetStream, PostgreSQL/GORM, Management, Static, OpenTelemetry и Prometheus. Core не запускает сетевые ресурсы сам по себе; прикладные модули подключаются явно.

Минимальный запуск:

```go
package main

import (
    backendcore "github.com/MrAmperage/ReportBoxBackendCore"
    "github.com/MrAmperage/ReportBoxBackendCore/app"
    "github.com/MrAmperage/ReportBoxBackendCore/management"
)

func main() {
    backendcore.Run(app.Info{Name: "example-service"}, management.Module())
}
```

Management по умолчанию слушает `127.0.0.1:7613`. Подключать RPC, static, messaging, DB и telemetry только при необходимости.

- [Архитектура](docs/architecture.md), [Core](docs/core.md)
- [ConnectRPC](docs/rpc.md), [NATS и JetStream](docs/nats.md), [PostgreSQL и GORM](docs/postgres.md)
- [Management](docs/management.md), [Telemetry](docs/telemetry.md), [Static](docs/static.md)
- [Примеры и план разработки](docs/development.md), [Release gate](docs/release.md)

Установка: `go get github.com/MrAmperage/ReportBoxBackendCore@<version>` (укажите опубликованный SemVer-тег). Поддерживаемый Go toolchain и версии зависимостей указаны в `go.mod`. Текущий статус выпуска — в [плане разработки](docs/development.md).

Для frontend-сервиса достаточно `static.Module()` с `STATIC_ROOT=dist/browser`, `STATIC_SPA=true`. По умолчанию listener — `127.0.0.1:8080`; наружная маршрутизация и TLS находятся вне библиотеки.
