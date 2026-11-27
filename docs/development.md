# Состояние разработки

[README](../README.md) · [Архитектура](architecture.md) · [Проверка и выпуск](release.md)

## Текущее состояние

Опубликована `v0.1.1`. Кандидат `v0.2.0` добавляет независимый модуль Auth; тег ещё не опубликован. Go и точный граф зависимостей определяются `go.mod`. Auth добавляет прямую зависимость `github.com/golang-jwt/jwt/v5` v5.3.1; `go.sum` должен быть обновлён реальным Go toolchain перед выпуском. Не менять опубликованный тег.

| Модуль | Состояние реализации |
|---|---|
| Core / Fx / config / logging / lifecycle / health | Реализован |
| RPC server/client | Реализован |
| NATS / JetStream | Реализованы |
| PostgreSQL / GORM | Реализованы |
| Management / Telemetry | Реализованы |
| Static | Реализован |
| Auth | Реализован кандидат: JWT, обязательные key/state providers, Identity, RPC policies и повторная проверка |

Реализованный модуль не означает подтверждённую готовность к production. Публичный API и ограничения — в профильных документах. [Auth](auth.md) и [ADR 0011](adr/auth.md) определяют контракт; AuthenticationClient, синхронизация, кеш и контракты конкретного AuthenticationService не входят в Core. Существующий RPC API сохранён.

## Проверки и блокеры

- В изолированном временном Go module на доступном Go 1.23.2 проверены реальные stdlib-файлы auth (`config`, `provider`, `identity`, `token`, `validator`, `token_test`): `go test -race -count=3 ./...` и `go vet ./...`. Это проверка конфигурации, preflight, копирования claims, типов ключей, повторной проверки состояния, таймаута и отображения ошибок; она не выполняет JWT crypto/registered claims parser, Fx или ConnectRPC integration.
- Полная сборка и тесты `auth/...` с JWT dependency и остальным проектом не подтверждены: DNS `proxy.golang.org` недоступен в рабочем окружении, установлен Go 1.23.2, целевой Go не загружается. Добавлены тесты подписей EdDSA/RS256/ES256, ошибок key/state, Fx, RPC policy и реального RPC listener. Они требуют проверки на целевом toolchain. `go.mod` содержит pin JWT; checksums не выдумывались. Выполнить `go mod tidy`, `go mod verify` и сохранить полученный `go.sum`.
- CI workflow отсутствует; полная интеграция с NATS/PostgreSQL, govulncheck и нагрузочные проверки независимо не подтверждены. Выполнить [release gate](release.md).

## Следующие задачи

1. Внести кандидат Auth, выполнить `go mod tidy`, `go mod verify`, `go test -race -count=3 ./auth/...`, `go vet ./...`, `go test -race ./...`, `go build ./...` и полный release gate. После подтверждения владельцем слить проверенные изменения и опубликовать `v0.2.0`.
2. В AuthenticationService/AuthenticationClient согласовать и реализовать token claims, ключи и ротацию, state provider (сессии и ревизии, cache TTL, invalidation, recovery, remote required, fail closed), прикладные permissions и тесты интеграции. В потребителях подключать Auth только для защищённых операций. Не переносить прикладные контракты в Core.
3. По мере реальной потребности развивать остальные инфраструктурные модули; не добавлять новые подсистемы заранее.

Документация описывает актуальное состояние, а не историю веток и коммитов. После публикации обновить состояние без изменения опубликованного тега.
