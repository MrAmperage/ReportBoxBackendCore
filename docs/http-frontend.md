# HTTP и микрофронтенды

[К описанию проекта](../README.md)

## 1. HTTP-инфраструктура

Использовать `net/http`; применение chi остаётся опциональным и определяется при этапе 2. Предлагаемый default — стандартный `ServeMux`, если его возможностей достаточно. Библиотека предоставляет маршрутизатор, настройки listener, middleware, регистрацию маршрутов через DI и lifecycle. Маршруты регистрируются до старта listener; явные дубликаты и пересечения с зарезервированными префиксами должны быть выявлены до bind. Предлагается регистрация деклараций routes через DI group и один сборщик router; порядок группы не влияет на приоритет маршрутов. Не обещать автоматическое обнаружение всех семантических пересечений произвольных шаблонов стороннего router. Для `ServeMux` конфликт/невалидный pattern может вызвать panic — валидировать декларации и переводить ошибки регистрации в startup error на узкой границе. Management и application используют разные экземпляры HTTP-сервера. Frontend не открывает третий listener.

| Listener | Default (предложение) | Пути |
|---|---|---|
| Application | `:8080` | `/api/v1/...`, `/frontend/...` |
| Management | `:8081` | `/health/live`, `/health/ready`, `/metrics` (с telemetry) |
| gRPC | `:9090` | protobuf RPC и gRPC health |

Management не публикуется в интернет. В Docker не пробрасывать management host port в публичную сеть; без сетевой изоляции явно bind на loopback. При развертывании в Docker/Kubernetes допускается bind на все интерфейсы внутри контейнера, но доступ ограничен сетью и политиками развертывания. Публичный TLS обычно завершается на gateway/reverse proxy. Не доверять произвольным `X-Forwarded-*`; задавать доверенные proxies. Опциональные debug endpoints закрыты по умолчанию.

Базовые настройки HTTP: `HTTP_ADDRESS=:8080`, `HTTP_READ_HEADER_TIMEOUT=5s`, `HTTP_IDLE_TIMEOUT=60s`, `HTTP_MAX_HEADER_BYTES=1048576`; отдельные префиксы `MANAGEMENT_` для технического listener. Таймауты тела и ответа выбираются по назначению маршрутов: для обычных API задать bounded deadlines и body limit, для больших uploads — отдельный лимит и скорость/время чтения. Для streaming маршрутов (SSE, длительные загрузки) нужна отдельная политика; общий короткий WriteTimeout может нарушить их работу. Ограничение размера тела, аутентификация, rate limits и request deadlines применяются на подходящем слое.

Listener открывается синхронно в OnStart (`net.Listen`); ошибка bind отменяет запуск. `Serve` выполняется в контролируемой goroutine, ошибки передаются механизму shutdown. Перед `Shutdown(ctx)` закрыть admission для новой работы и снять readiness. `Shutdown(ctx)` ожидает активные запросы, при превышении выделенного транспортного бюджета — `Close()` и отмена lifetime context. Не отменять рабочие контексты активных запросов до начала graceful drain; отслеживать долгоживущие потоки и выделять им собственный stop signal. Hijacked connections (WebSocket и подобные) требуют отдельной регистрации и закрытия.

`GET /health/live` — 200 для работающего процесса, `GET /health/ready` — 200 только при readiness=true, иначе 503. Ответ JSON с минимальным статусом, `Cache-Control: no-store`. Проверки не раскрывают секреты. Metrics — только при включённом соответствующем модуле.

## 2. Frontend-модуль

Цель: каждый UI-сервис раздаёт свою сборку микрофронтенда одновременно с backend. Ядро не зависит от Angular, Webpack или Native Federation. Оно принимает `fs.FS` и обслуживает статические файлы. Публичный API — предложение:

```go
frontend.Module(
    frontend.WithFS(dist),
    frontend.WithPrefix("/frontend/"),
)
```

Локальная разработка: использовать безопасный directory adapter (например `os.Root.FS()` при выбранной поддерживаемой версии Go); произвольный `os.DirFS` допустим только как доверенный источник с явно описанной границей доверия; для встраивания использовать `embed.FS` с `fs.Sub`. Выбирает режим сервис, не библиотека. Модуль активируется только при явном подключении. Проверить FS, корректность префикса, наличие настраиваемых entry-файлов на старте. Путь должен оканчиваться `/`; не пересекаться с API и техническими маршрутами.

```go
// Пример в пакете сервиса, где реально расположен каталог dist.
//go:embed dist
var embedded embed.FS

func Files() (fs.FS, error) {
    return fs.Sub(embedded, "dist")
}
```

Сборка Angular должна предшествовать компиляции Go. Путь `dist` соответствует фактическому output Angular; в некоторых конфигурациях файлы расположены в подкаталоге `browser`. Этот путь задаёт сервис.

## 3. Публикация и интеграция с Shell

Предпочтительный вариант для ReportBox — один публичный origin с маршрутизацией по имени сервиса. Например:

```text
https://app.example.com/services/reports/frontend/remoteEntry.json
https://app.example.com/services/reports/frontend/chunk-ABC123.js
https://app.example.com/services/reports/api/v1/reports
```

Gateway/reverse proxy удаляет внешний префикс `/services/reports`, сохраняет остальной путь. Сервис отдаёт файлы с `/frontend/`. Сборка remote должна разрешать ссылки на chunks/assets с корректной публичной базой; проверять на реальном gateway, а не только на localhost. Альтернатива — разные origins с явно разрешённым CORS. Shell отвечает за загрузку remote, интеграцию маршрутов, совместимость shared dependencies и отображение ошибки при недоступности remote.

Выбор Module Federation (`remoteEntry.js`) или Native Federation (`remoteEntry.json`) и механизм обнаружения remote пока не утверждены. Ядро поддерживает оба способа как static assets и не навязывает формат manifest. В v0.1 достаточно конфигурации URL в Shell/gateway, без нового registry-сервиса.

## 4. Безопасная раздача

- Только GET и HEAD; корректные Content-Type для JS/MJS, CSS, JSON, WASM и иных ресурсов; `X-Content-Type-Options: nosniff`.
- Не отдавать directory listings, скрытые файлы, карты исходников по умолчанию, конфиги, секреты и файлы за пределами корня. `os.DirFS` сам по себе не является защитой от symlink escape. Для поддерживаемого режима каталога использовать traversal-resistant root (`os.OpenRoot` + `Root.FS()` либо эквивалент с проверенной безопасностью), закрывать root при остановке. Для произвольного переданного `fs.FS` безопасность внутренней реализации — ответственность поставщика; ядро проверяет имена и запрещённые пути, но не обещает изоляцию недоверенного FS. Артефакты должны содержать только публичные файлы; не полагаться исключительно на blacklist. Тестировать encoded traversal, symlink escape и отсутствие directory listing.
- Не делать SPA fallback для remote: отсутствующий chunk или manifest возвращает 404, а не index.html. Для standalone SPA — только отдельный явно включённый режим, не маскирующий ошибки assets/API.
- Использовать условные запросы и диапазоны там, где они корректно поддерживаются FS. Для embed при необходимости вычислять стабильный ETag из содержимого или сборочного manifest, а не из текущего времени.
- Same-origin по умолчанию. Для cross-origin — allowlist origins, `Vary: Origin` для динамического CORS; не использовать wildcard вместе с credentials. Frontend assets не содержат секретов; загрузка UI не заменяет серверную авторизацию API.

## 5. Cache policy и обновление

| Ресурс | Cache-Control |
|---|---|
| remoteEntry.js / remoteEntry.json / mf-manifest.json / index.html | `no-cache` |
| Доказанно content-hashed assets | `public, max-age=31536000, immutable` |
| Остальные файлы | `no-cache` |
| Ошибки 4xx/5xx | `no-store` |

Нельзя считать все JS/CSS файлы хешированными; политика использует проверенный manifest или явно настроенный шаблон сборки. Версионирование frontend/backend одной сборкой обеспечивает соответствие в артефакте, но не устраняет проблемы rolling updates, старых вкладок и старых chunks. Для production предусмотреть сохранение прежних assets на период совместимости либо маршрутизацию по версии сборки; при ошибке загрузки Shell предлагает обновить страницу. Предлагаемый вариант: неизменяемый build ID в URL remote и всех его assets, хранение текущего и предыдущих наборов на общем статическом origin/repository (или version-aware routing); entry URL выбирается manifest Shell. Для локального одиночного сервиса допустима простая схема с обязательным reload при несовместимости. При rolling deployment один новый binary с embed не гарантирует доступность старых chunks на любой реплике. Не включать immutable для файла, чей URL может обозначать другое содержимое. Конкретная production-стратегия rollout утверждается отдельно до многорепличного развертывания.

## 6. Контейнеризация

Рекомендуемый вариант — multi-stage Docker build: Node stage (`npm ci`, build), Go stage (копирование результата в embed-каталог, сборка с version/commit), минимальный runtime stage, non-root, CA certificates и read-only FS где возможно. Версии базовых образов фиксируются после выбора версии Angular/Go. Альтернативно: копировать dist как read-only каталог в тот же образ. Backend и frontend публикуются одной версией, один сервис — один образ; frontend без UI-сервисов не обязателен.

Критерии готовности: работоспособность двух способов FS, GET/HEAD, Content-Type, cache policy, CORS, traversal и symlink tests, отсутствие directory listing, корректный 404, успешная загрузка remote и его chunks через реальный префикс gateway, graceful shutdown.

Источник: [net/http](https://pkg.go.dev/net/http), [embed](https://pkg.go.dev/embed), [io/fs](https://pkg.go.dev/io/fs), [os.Root](https://go.dev/blog/osroot).
