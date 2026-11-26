# ADR 0007 — Management

[Архитектура](../architecture.md) · [API](../management.md)

Статус: согласован отдельный технический listener и порт 7613; реализация-кандидат подготовлена для ветки Management от `e9d1a4b`. Полная проверка на целевом Go и публикация кандидата ещё не подтверждены. Уточняет ADR 0003 в части management port и endpoints.

| Решение | Мотивация |
|---|---|
| `127.0.0.1:7613` по умолчанию | Указание владельца; безопасный локальный доступ, отдельно от RPC |
| Явный IP и `MANAGEMENT_ALLOW_REMOTE=true` для non-loopback | Дополнительная защитная политика реализации; private network без публичного proxy |
| `/livez`, `/readyz`, `/healthz`, опциональный `/metrics` | Разделение process liveness, обязательной readiness, диагностики и метрик |
| `/healthz` 503 при любом failed/incomplete check | Диагностика не смешивается с readiness; не выполнять второй probe |
| Только GET/HEAD, лимиты, no-store, стандартный net/http | Минимальная поверхность; без REST, pprof, CORS, TLS/auth внутри модуля |
| Существующий `health.Registry`, `lifecycle.Participant` | Нет повторной модели состояния или прямой зависимости от DB/NATS |
| Optional Fx value `management.MetricsHandler` | Будущий Telemetry поставляет native HTTP handler, management не импортирует Prometheus |

Ограничения: health checks и metrics handler должны соблюдать context; сервер не ограничивает число TCP connections глобально, сеть защищает deployment. При таймауте drain отменяет запросы и принудительно закрывает соединения; не обещает остановку произвольного кода. TLS для удалённого доступа и реальный deployment не проверены. Старые проектные `/health/live`, `/health/ready`, порт 8081 заменены согласованным контрактом.
