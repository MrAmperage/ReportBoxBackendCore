# ADR 0007 — Management

[Архитектура](../architecture.md) · [API](../management.md)

| Решение | Мотивация |
|---|---|
| `127.0.0.1:7613` по умолчанию | Безопасный локальный доступ, отдельно от RPC |
| Явный IP и `MANAGEMENT_ALLOW_REMOTE=true` для non-loopback | private network без публичного proxy |
| `/livez`, `/readyz`, `/healthz`, опциональный `/metrics` | Разделение process liveness, обязательной readiness, диагностики и метрик |
| Loopback Host ограничен адресом listener/localhost и фактическим портом; absolute-form запрещён | Защита от DNS rebinding без ограничения DNS-имён в явно разрешённой приватной сети. |
| Panic отдельного health check считается unhealthy | Стабильный HTTP 503 вместо оборванного ответа, без утечки значения panic; другие probes сохраняются. |
| `/healthz` 503 при любом failed/incomplete check | Диагностика не смешивается с readiness; не выполнять второй probe |
| Только GET/HEAD, лимиты, no-store, стандартный net/http | Минимальная поверхность; без REST, pprof, CORS, TLS/auth внутри модуля |
| Существующий `health.Registry`, `lifecycle.Participant` | Нет повторной модели состояния или прямой зависимости от DB/NATS |
| Optional Fx value `management.MetricsHandler` | Telemetry поставляет native HTTP handler, management не импортирует Prometheus |

Ограничения: health checks и metrics handler должны соблюдать context; сервер не ограничивает число TCP connections глобально, сеть защищает deployment. При таймауте drain отменяет запросы и принудительно закрывает соединения; не обещает остановку произвольного кода.
