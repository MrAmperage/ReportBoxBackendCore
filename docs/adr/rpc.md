# ADR 0004 — Реализация RPC

[ADR 0003](edge.md) · [API RPC](../rpc.md)

Статус: реализация-кандидат в ветке RPC; детали требуют review, полная сборка не подтверждена.

| Решение | Обоснование |
|---|---|
| ConnectRPC 1.19.1, Protobuf 1.36.9, текущий Go 1.27 | Стабильный API v1; штатный `http.Protocols` для HTTP/2/h2c, без отдельного grpc-go server и REST framework. |
| `ServerModule`, `ClientModule`, `ProvideHandler`, `Handler` | Независимые модули Fx, обязательная передача options в generated handler, без ранних сетевых операций. |
| `Authorize` или явный `AllowUnauthenticated` | Нет неявно открытых прикладных сервисов. Публичный allowlist Caddy не заменяет auth. |
| Loopback `127.0.0.1:9090` default, TLS/mTLS или явный plaintext в production | Минимизировать случайное открытие порта; совместимость с Caddy и Docker. |
| `Target(endpoint, protocol, extra...)` | Shared HTTP transports, явный gRPC/Connect/gRPC-Web, origin validation; без service discovery до потребности. |
| `ShutdownSignal` в DI независимо от server | Потоковые handlers могут завершаться до закрытия транспорта без DI cycle. |
| Deadline 30s, message 4 MiB, concurrency 128, безопасные error codes | Изменяемые консервативные defaults; подробные ошибки и OTel позже. |
| Полный архив + CI workflow для RPC | Владелец публикует самостоятельно; сохраняются все файлы основной ветки. |

Ограничения: нет automatic retry, Caddy route generation, application auth/JWT, distributed tracing, reflection или service discovery. Пример ручной типизированной регистрации на Protobuf well-known types, генерация stubs/TS ещё не утверждена. TLS/mTLS и браузерная интеграция требуют реальных тестов. `go.sum` содержит upstream checksums; нужна проверка `go mod tidy` и полный прогон Go 1.27. Не считать релизом.
