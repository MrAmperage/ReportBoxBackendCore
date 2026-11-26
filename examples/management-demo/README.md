# Пример Management

Из корня репозитория:

```sh
go run ./examples/management-demo
```

В другом терминале:

```sh
curl -i http://127.0.0.1:7613/livez
curl -i http://127.0.0.1:7613/readyz
curl -i http://127.0.0.1:7613/healthz
curl -i http://127.0.0.1:7613/metrics  # 404 без Telemetry
```

До завершения startup readiness=false; после старта без обязательных зависимостей readiness=true. Остановить Ctrl+C. Пример не запускает RPC, PostgreSQL или NATS. См. [документацию](../../docs/management.md).
