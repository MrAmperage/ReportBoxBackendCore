# Telemetry demo

Из корня репозитория:

```bash
go run ./examples/telemetry-demo
curl http://127.0.0.1:7613/metrics
curl http://127.0.0.1:7613/readyz
```

Без OTLP endpoint включены Prometheus metrics. Пример регистрирует native `reportbox_demo_value` в per-app registry. Для trace export задайте переменные `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_TRACES_SAMPLER_ARG`; для HTTP в доверенной сети явно `TELEMETRY_ALLOW_PLAINTEXT=true`. Пример сам не генерирует trace spans; сценарий RPC/OTLP проверяется тестами `go test -race ./telemetry ./internal/observe ./rpc`.

[Telemetry API](../../docs/telemetry.md).
