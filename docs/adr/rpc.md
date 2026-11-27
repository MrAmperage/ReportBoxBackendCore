# ADR 0004 — Реализация RPC

[ADR 0003](edge.md) · [API RPC](../rpc.md)

Статус: RPC после ревизии перенесён в `main` (`b077f03`). Владелец сообщил о локальной проверке; конкретные команды и логи не предоставлены. CI отсутствовал, отдельные TLS/mTLS и browser/ingress проверки не подтверждены.

| Решение | Обоснование |
|---|---|
| ConnectRPC 1.19.1, Protobuf 1.36.9, текущий Go 1.27 | Стабильный API v1; штатный `http.Protocols` для HTTP/2/h2c, без отдельного grpc-go server и REST framework. |
| `ServerModule`, `ClientModule`, `ProvideHandler`, `Handler` | Независимые модули Fx, обязательная передача options в generated handler, без ранних сетевых операций. |
| `Authorize` или явный `AllowUnauthenticated` | Нет неявно открытых прикладных сервисов. Публичный allowlist внешней маршрутизации не заменяет auth. |
| Loopback `127.0.0.1:9090` default, TLS/mTLS или явный plaintext в production | Минимизировать случайное открытие порта; совместимость с контейнерным deployment. |
| `Target(endpoint, protocol, extra...)` | Shared HTTP transports, явный gRPC/Connect/gRPC-Web, origin validation; без service discovery до потребности. |
| `ShutdownSignal` в DI независимо от server | Потоковые handlers могут завершаться до закрытия транспорта без DI cycle. |
| Deadline 30s, message 4 MiB, concurrency 128, безопасные error codes | Изменяемые консервативные defaults; подробные ошибки и OTel позже. |
| Допуск RPC только в фазе Running | Не принимать business calls до полной активации; проверка фазы без выполнения dependency probes. |
| Lifetime исходящих RPC связан с закрытием клиента; authority pinned | Отмена активных запросов/streams при Close, защита от подмены Host. |
| Полный архив + CI workflow для RPC | Владелец публикует самостоятельно; сохраняются все файлы основной ветки. |

Ограничения: нет automatic retry, генерации конфигурации внешнего proxy, application auth/JWT, distributed tracing, reflection или service discovery. Пример ручной типизированной регистрации на Protobuf well-known types, генерация stubs/TS ещё не утверждена. TLS/mTLS и браузерная интеграция требуют реальных тестов. `go.sum` содержит upstream checksums; нужна проверка `go mod tidy` и полный прогон Go 1.27. Не считать релизом.
