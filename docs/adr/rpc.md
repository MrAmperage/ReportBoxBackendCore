# ADR 0004 — ConnectRPC и маршрутизация

[Архитектура](../architecture.md) · [API RPC](../rpc.md)

| Решение | Обоснование |
|---|---|
| ConnectRPC и Protobuf, версии в `go.mod` | Стабильный API v1; штатный `http.Protocols` для HTTP/2/h2c, без отдельного grpc-go server и REST framework. |
| `ServerModule`, `ClientModule`, `ProvideHandler`, `Handler` | Независимые модули Fx, обязательная передача options в generated handler, без ранних сетевых операций. |
| `Authorize` или явный `AllowUnauthenticated` | Нет неявно открытых прикладных сервисов. Публичный allowlist внешней маршрутизации не заменяет auth. |
| Loopback `127.0.0.1:9090` default, TLS/mTLS или явный plaintext в production | Минимизировать случайное открытие порта; совместимость с контейнерным deployment. |
| `Target(endpoint, protocol, extra...)` | Shared HTTP transports, явный gRPC/Connect/gRPC-Web, origin validation; без service discovery до потребности. |
| `ShutdownSignal` в DI независимо от server | Потоковые handlers могут завершаться до закрытия транспорта без DI cycle. |
| Deadline 30s, message 4 MiB, concurrency 128, безопасные error codes | Изменяемые консервативные defaults; без передачи чувствительных данных. |
| Допуск RPC только в фазе Running | Не принимать business calls до полной активации; проверка фазы без выполнения dependency probes. |
| Lifetime исходящих RPC связан с закрытием клиента; authority pinned | Отмена активных запросов/streams при Close, защита от подмены Host. |
| Внешняя маршрутизация и TLS termination вне библиотеки | Не зависеть от реализации ingress; явно разрешать публичные процедуры и проверять права в самом сервисе. |
| `MaxHeaderBytes` с запасом 4096 байт | Исключить integer overflow в net/http. |

Ограничения: нет automatic retry, service discovery, reflection, прикладной модели auth/JWT или генерации конфигурации ingress. Универсальный [Auth](auth.md) подключается опционально через существующий hook; конкретная политика остаётся в сервисе. Distributed tracing опционален через Telemetry. Browser Connect поддерживает unary и server streaming; client/bidi streaming не гарантируется fetch-транспортом. Native gRPC требует HTTP/2; h2c разрешается только согласно политике конфигурации в доверенной сети. Генерация stubs и публикация Protobuf-контрактов принадлежат сервисам. UI и backend развёртываются независимо; [Static](static.md) не меняет RPC. См. [Frontend deployment](../http-frontend.md).
