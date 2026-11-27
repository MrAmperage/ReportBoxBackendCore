# Static — HTTP-раздача готовых frontend-файлов

[Архитектура](architecture.md) · [ADR 0010](adr/static.md) · [Frontend deployment](http-frontend.md)

## API

```go
import (
    backendcore "github.com/MrAmperage/ReportBoxBackendCore"
    "github.com/MrAmperage/ReportBoxBackendCore/app"
    "github.com/MrAmperage/ReportBoxBackendCore/static"
)

func main() {
    cfg := static.DefaultConfig()
    cfg.Root = "/app/public"
    cfg.SPA = true
    backendcore.Run(app.Info{Name: "reportbox-frontend-service"}, static.Module(cfg))
}
```

`static.Module()` читает ENV, `static.Module(cfg)` принимает один полный Config и не читает настройки модуля из ENV. `DefaultConfig()`, `LoadConfig()`, `Config.Validate()` не открывают файлы и сокеты. `*static.Server` доступен через Fx; `Address()` возвращает фактический bound address после Open и пустую строку после Close. `static.ParticipantName = "static.server"` — имя lifecycle dependency. Не требуется RPC, Management, DB, broker, Telemetry.

## Конфигурация

| ENV | Default | Назначение |
|---|---|---|
| `STATIC_ROOT` | обязательный | Доверенный каталог сборки (например `dist/browser`) |
| `STATIC_ADDRESS` | `127.0.0.1:8080` | Явный IP:port, `0` для тестов |
| `STATIC_ALLOW_REMOTE` | `false` | Явное разрешение non-loopback HTTP |
| `STATIC_SPA` | `false` | SPA fallback; требует `index.html` |
| `STATIC_READ_HEADER_TIMEOUT` | `5s` | Чтение заголовков/запроса |
| `STATIC_WRITE_TIMEOUT` | `30s` | Запись ответа |
| `STATIC_IDLE_TIMEOUT` | `30s` | Keep-alive idle |
| `STATIC_MAX_HEADER_BYTES` | `16384` | HTTP header limit |
| `STATIC_MAX_CONCURRENT` | `64` | Максимум активных обработчиков, не TCP connections |

Для Docker deployment можно задать `STATIC_ROOT=/app/public`, `STATIC_ADDRESS=0.0.0.0:8080`, `STATIC_ALLOW_REMOTE=true`, `STATIC_SPA=true`. Не публиковать plaintext напрямую в недоверенную сеть. TLS и маршрутизация — отдельная ответственность deployment. Root должен содержать исключительно публичную сборку, не исходники, конфиги, ключи, архивы, данные пользователей или credentials. Не использовать корень проекта, ОС или домашний каталог. Read-only mount/образ; не менять дерево во время работы. Не размещать внутри root bind mounts с приватными данными.

## HTTP-контракт

- Для loopback listener `Host` должен содержать IP фактически открытого listener или `localhost` и его фактический порт (для HTTP port 80 порт можно опустить). Иначе 421, до доступа к файлам. Заголовки Forwarded/X-Forwarded-Host не используются для этой проверки. При reverse proxy на loopback передавать допустимый upstream Host; при явно разрешённом non-loopback listener доменные Host допускаются, внешний ingress сам ограничивает допустимые authority.
- GET/HEAD без тела; остальные методы — 405. Абсолютная форма запроса и тело — 400. `/` отдаёт `index.html`, если он есть. Не выполняется directory listing или автоматический redirect.
- Существующие обычные файлы обслуживаются по пути внутри root, query не участвует в lookup. Поддерживаются MIME JS/MJS/CSS/JSON/WASM/HTML, стандартный `ServeContent`, Last-Modified, conditional GET/304 и Range/206. `Cache-Control: no-cache`, `X-Content-Type-Options: nosniff`. ETag, автоматический immutable cache и precompressed negotiation не реализованы.
- Запрещены dot segments, скрытые компоненты, `.map` (независимо от регистра), обратный slash, двоеточие, повторное percent-encoding, управляющие символы и завершающие пробелы/точки. Symlinks и специальные файлы отклоняются при проверке дерева на startup; `os.Root` ограничивает последующие открытия файлов корнем. Это не sandbox для недоверенного локального writer; root должен быть неизменяемым и доверенным.
- SPA включается явно; только при ошибке отсутствия файла и пути без точек в компонентах и `Accept: text/html` с положительным q (если задан `Sec-Fetch-Mode`, только один заголовок со значением `navigate`) отдаётся `index.html`. Существующий каталог или файл с ошибочным завершающим `/` не получает fallback; возвращается 404. Не применяется к `/assets`, `/microfrontends`, `/api`, `/rpc`, `/management`, `/metrics`, `/livez`, `/readyz`, `/healthz`, `/static` и их подпутям, к отсутствующим файлам с расширением и RPC procedure paths с точками. Иные extensionless asset prefixes следует размещать в `/assets` или отключить SPA. `Vary: Accept, Sec-Fetch-Mode`.
- Ошибки не раскрывают путь на диске. Нет CORS, TLS, API, reverse proxy, auth, upload, dynamic routes или directory browsing. Frontend-сервис публикует только публичные файлы; management endpoints принадлежат отдельному listener.

## Lifecycle

Open проверяет root и index для SPA, затем синхронно bind TCP; при ошибке закрывает открытые ресурсы. Activate запускает `Serve` под управлением coordinator. До Running, при перегрузке и после Quiesce запросы отклоняются с 503. Не выполняются DB/NATS probes на каждый запрос. Admission и Quiesce синхронизированы одним mutex, счётчик активных handlers уменьшается в defer. Drain резервирует до 20% оставшегося бюджета (максимум 1s) для принудительной остановки; при исчерпании graceful deadline отменяет lifetime context и закрывает HTTP connections, ждёт завершения уже принятых handlers в пределах общего deadline. Close повторно ждёт Serve и handlers перед закрытием root в пределах доступного контекста. При исчерпании deadline возвращается ошибка; root закрывается даже при незавершённом обработчике. Отмена не гарантирует немедленное завершение произвольного зависшего filesystem I/O. Неожиданное завершение Serve вызывает `lifecycle.ReportFailure` с безопасной ошибкой. Повторный запуск того же экземпляра не поддерживается.

## Пример и проверки

См. [static-demo](../examples/static-demo/README.md). Конфигурация, traversal, SPA, cache/MIME/range, Host/DNS rebinding, lifecycle (в том числе forced drain с активным handler и перегрузкой), bind error и symlink rejection покрыты тестами; `go test -race ./static` проверяет полный пакет на Go 1.27.4+. Статический handler и config можно отдельно тестировать без Fx/нового Go toolchain; это не подтверждает работу os.Root и реального lifecycle.
