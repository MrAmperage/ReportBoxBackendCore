# ReportBoxBackendCore

Базовая Go-библиотека для микросервисов ReportBox: Fx, ConnectRPC, NATS/JetStream, PostgreSQL/GORM, Management, OpenTelemetry и Prometheus. Core не запускает сетевые ресурсы сам по себе; прикладные модули подключаются явно.

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

Management по умолчанию слушает `127.0.0.1:7613`. Подключать RPC, messaging, DB и telemetry только при необходимости.

- [Архитектура](docs/architecture.md), [Core](docs/core.md)
- [ConnectRPC](docs/rpc.md), [NATS и JetStream](docs/nats.md), [PostgreSQL и GORM](docs/postgres.md)
- [Management](docs/management.md), [Telemetry](docs/telemetry.md)
- [Примеры и план разработки](docs/development.md), [Release gate](docs/release.md)

Подготовка к первому релизу `v0.1.0`: статус и обязательные проверки приведены в плане разработки. Не считать кандидат выпущенной версией до создания тега.
