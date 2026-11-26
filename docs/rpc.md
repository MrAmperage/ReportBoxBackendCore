# RPC — ConnectRPC

[Архитектура](architecture.md) · [Контракты и сообщения](messaging.md) · [ADR RPC](adr/rpc.md)

Статус: RPC после ревизии перенесён в `main` (`b077f03`). Владелец сообщил о локальной проверке; конкретные команды и логи не предоставлены. CI отсутствовал, отдельные TLS/mTLS и Caddy/browser проверки не подтверждены.

## 1. Подключение

```go
backendcore.Run(
    app.Info{Name: "reportbox-auth-service"},
    rpc.ServerModule(),
    rpc.ClientModule(),
    rpc.ProvideHandler(newAuthHandler),
)
```

Server и client подключаются независимо. Без этих модулей Core не создаёт RPC listeners и transports. `ServerModule()` и `ClientModule()` читают ENV; передача одного полного `ServerConfig`/`ClientConfig` отключает ENV для соответствующего модуля. Использовать `DefaultServerConfig()`/`DefaultClientConfig()` как основу явных настроек. Некорректное значение не заменяется молча. `go.mod` сохраняет Go 1.27, Fx 1.24.0; добавлены ConnectRPC 1.19.1 и Protobuf 1.36.9.

## 2. Регистрация сервиса и авторизация

Сервис владеет собственными `.proto`, generated handlers и бизнес-логикой. Пример адаптера к сгенерированному `authv1connect.NewAuthServiceHandler` (имена пакетов условны):

```go
func newAuthHandler(service *AuthService, auth *Authorization) rpc.Handler {
    return rpc.Handler{
        Build: func(options ...connect.HandlerOption) (string, http.Handler) {
            return authv1connect.NewAuthServiceHandler(service, options...)
        },
        Authorize: auth.Authorize,
    }
}
```

`Authorize` имеет сигнатуру `func(context.Context, string, http.Header) (context.Context, error)`. Вызывается для unary и streaming handler. Возвращаемый контекст должен быть производным от исходного, чтобы сохранить deadline/cancellation и request ID; в него можно добавить проверенную identity. Не доверять клиентским заголовкам без проверки. Ошибки авторизации возвращать как `connect.NewError(connect.CodeUnauthenticated/CodePermissionDenied, err)`.

Ровно один из `Authorize` и `AllowUnauthenticated: true` обязателен. Последний допустим для сознательно выбранных открытых процедур или отдельного demo. Если один service содержит Login и защищённые методы, `Authorize` должен проверять полное имя процедуры и разрешать только согласованные исключения. Caddy дополнительно ограничивает публичные маршруты; отсутствие авторизации не означает разрешение публичной маршрутизации. Анонимность и внешний allowlist — разные понятия. Дублирующие префиксы сервисов отклоняются до bind. `Build` обязан передавать все options generated handler и не открывать ресурсы. Регистрация делается до запуска, динамический роутер не требуется. HTTP dispatch принимает только canonical RPC procedure paths; нет REST, статики, автоматического fallback или reflection.

`rpc.InterceptorGroup` — дополнительная Fx value group типа `connect.Interceptor`. Порядок элементов группы не гарантирован; если нужен определённый порядок, предоставить один составной interceptor. Встроенный interceptor является внешней обёрткой: auth hook, recovery, журналирование процедуры/кода/времени/request ID. Запросы, response payload, заголовки, raw driver errors и panic values не логируются. Ошибки по умолчанию преобразуются в безопасный текст кода; `CodeUnknown`/неизвестные ошибки → `CodeInternal`. Error details и metadata не передаются клиенту. Политика безопасных прикладных сообщений может быть добавлена позже. `rpc.RequestID(ctx)` возвращает серверный случайный идентификатор, ответ получает `X-Request-ID`; входящий ID не считается доверенным. Нет автоматической identity propagation, retry или OTel — точки расширения позволяют подключить это отдельно. Стандартный raw `http.Server.ErrorLog` отключён, фатальный отказ listener и ошибки процедур логируются без сырых TLS/HTTP данных.

## 3. Клиент

`*rpc.Client` — владелец переиспользуемых transport/client. После Open можно подготовить типизированный клиент; соединения открываются только при реальном запросе. Для DI-компонентов, использующих клиент при startup, регистрация должна иметь зависимость `rpc.client` и создавать target не раньше Activate. Не выполнять RPC в DI constructors. Пример со сгенерированным клиентом:

```go
target, err := client.Target("http://127.0.0.1:9090", rpc.GRPC)
if err != nil { return err }
auth := authv1connect.NewAuthServiceClient(
    target.HTTPClient, target.BaseURL, target.Options...,
)
```

Протокол выбирается явно: `rpc.GRPC` (рекомендуется Go-to-Go), `rpc.Connect`, `rpc.GRPCWeb`. `Target` принимает только HTTP(S) origin без credentials, path, query и fragment. Клиент привязан к origin: проверяются URL scheme/host, HTTP Host (authority), отсутствие credentials и opaque URL. Redirects отключены, HTTP proxy из ENV не применяется. Для TLS включены HTTP/1.1 и HTTP/2; plaintext gRPC использует h2c prior knowledge, остальные plaintext режимы — HTTP/1.1. Для HTTP plaintext outside loopback требуется явное разрешение; TLS/mTLS предпочтительны между хостами. Client TLS keypair несовместим с plaintext target. Дополнительные `connect.ClientOption` можно передать в `Target`, например interceptors; не задавать ими конфликтующий wire protocol и не обходить лимиты. Target не является проверкой здоровья remote. Создавать generated client один раз для endpoint, не создавать transport на запрос. Передавать контекст с deadline в каждый вызов; общий `http.Client.Timeout` ограничивает также streaming body. При Close не принимаются новые вызовы, отменяется общий lifetime всех исходящих запросов, включая чтение открытых streams, и закрываются idle connections. Контекст вызова сохраняется; отмена lifetime связана с ним до EOF/Close body. При штатном shutdown владельцы должны завершить обработчики до Close клиента.

## 4. Конфигурация

| ENV сервера | Default |
|---|---|
| `RPC_ADDRESS` | `127.0.0.1:9090` |
| `RPC_ALLOW_PLAINTEXT` | `false` |
| `RPC_TLS_CERT_FILE`, `RPC_TLS_KEY_FILE` | пусто |
| `RPC_CLIENT_CA_FILE` (mTLS trust на сервере) | пусто |
| `RPC_READ_HEADER_TIMEOUT` | `5s` |
| `RPC_IDLE_TIMEOUT` | `60s` |
| `RPC_CALL_TIMEOUT` | `30s` |
| `RPC_MAX_HEADER_BYTES` | `1048576` |
| `RPC_MAX_MESSAGE_BYTES` | `4194304` |
| `RPC_MAX_CONCURRENT` | `128` |

В development plaintext разрешён на loopback даже при `RPC_ALLOW_PLAINTEXT=false`; в production (значение `SERVICE_ENVIRONMENT=production`) или при non-loopback bind требуется TLS или явное `true`. При TLS certificate/key задаются вместе, `RPC_CLIENT_CA_FILE` включает обязательную проверку клиентских сертификатов. Минимальная TLS 1.2. Никакого `InsecureSkipVerify`. Для host Caddy + Docker backend на том же хосте допустим явный plaintext в защищённой сети: `RPC_ADDRESS=:9090`, `RPC_ALLOW_PLAINTEXT=true`, host publish только `127.0.0.1:9090:9090`. Не публиковать сам контейнерный порт наружу. Для Caddy h2c необходим соответствующий upstream transport; обычный HTTP/1.1 достаточен для Connect unary/browser server streaming, но не для native gRPC.

| ENV клиента | Default |
|---|---|
| `RPC_CLIENT_ALLOW_PLAINTEXT` | `false` |
| `RPC_CLIENT_TRUST_CA_FILE` | пусто (system trust) |
| `RPC_CLIENT_TLS_CERT_FILE`, `RPC_CLIENT_TLS_KEY_FILE` | пусто |
| `RPC_CLIENT_DIAL_TIMEOUT` | `5s` |
| `RPC_CLIENT_CALL_TIMEOUT` | `30s` |
| `RPC_CLIENT_IDLE_TIMEOUT` | `90s` |
| `RPC_CLIENT_MAX_IDLE_CONNS` | `100` |
| `RPC_CLIENT_MAX_CONNS_PER_HOST` | `32` |
| `RPC_CLIENT_MAX_MESSAGE_BYTES` | `4194304` |

Limits положительные; лимит сообщений действует на каждое сообщение, не общий объём бесконечного stream. `RPC_MAX_CONCURRENT` — количество допущенных HTTP handlers; перегрузка получает HTTP 503. Call timeout кооперативно отменяет handler context; I/O deadlines устанавливаются, если доступны в net/http. На transport timeout клиент может получить транспортную ошибку, а не гарантированный typed DeadlineExceeded. Для длительных streams выбрать timeout осознанно, не ставить короткий общий `WriteTimeout` на все соединения.

## 5. Lifecycle

Оба модуля реализуют `lifecycle.Participant` (`rpc.server`, `rpc.client`). Сервер: Open проверяет сертификаты и синхронно bind; Activate запускает Serve; health check `rpc.server` required; ошибка listener → `lifecycle.ReportFailure`. Допуск business RPC возможен только при фазе Core Running (`health.Registry.Running()`, без выполнения dependency probes), открытом admission и свободном concurrency slot. Во время startup/stop возвращается HTTP 503. Это не замена readiness checks. Quiesce закрывает admission и `ShutdownSignal.Done()`, Drain завершает HTTP requests/streams; 20% оставшегося deadline, максимум 1s, резервируется для forced close и cancellation. Close закрывает listener, отменяет lifetime context. Активные обработчики отслеживаются; произвольный некооперативный Go-код принудительно остановить невозможно. Клиентские transports готовятся в Open и закрываются после Drain всех участников. `rpc.ShutdownSignal` можно внедрить непосредственно в handler для graceful завершения долгого stream без циклической зависимости от `rpc.Server`.

```go
func stream(ctx context.Context, signal *rpc.ShutdownSignal) error {
    select {
    case <-ctx.Done(): return ctx.Err()
    case <-signal.Done(): return nil
    }
}
```

Readiness и lifecycle coordinator остаются в Core. RPC не открывает management HTTP и не реализует gRPC health reflection. Публичный и внутренний RPC разделяются deployment allowlist и авторизацией сервиса, не разными listener в текущем API.

## 6. Проверка и примеры

[Два процесса](../examples/rpc-demo/README.md) используют один Protobuf contract и типизированные нативные Go handlers/clients на готовом well-known type. Service stubs и TypeScript генерация не включены: это отдельная задача прикладных сервисов. Buf/protoc и registry контрактов остаются открытым выбором. ConnectRPC Go reference: https://connectrpc.com/docs/go/getting-started/ ; Go HTTP protocols: https://pkg.go.dev/net/http#Protocols .

Тесты: config/validation, отсутствующие TLS-файлы; реальный HTTP/1.1/HTTP/2 loopback, Connect/gRPC/gRPC-Web, errors/recovery, auth, message limits, stream drain, duplicate route, bind failure, client-only и origin validation. Добавлены тесты на подмену Host, отмену чтения активного HTTP stream при закрытии клиента и запрет RPC до окончания startup. Локально Go 1.23.2: config/security/origin tests и health прошли с race detector и vet, включая реальный HTTP/1.1 loopback origin test; Go 1.27.0 и внешние зависимости не скачиваются в этой среде. Полный прогон, CI, реальные TLS/mTLS handshake, Caddy и браузерная интеграция пока не подтверждены. Перед merge:

```sh
go mod tidy
go mod verify
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

## Telemetry

При подключении `telemetry.Module()` server/client unary и streaming interceptors передают W3C trace context и записывают длительность и код завершения generated RPC. Без модуля поведение транспорта не меняется. При остановке клиента зарегистрированные streaming observations завершаются до закрытия Telemetry; закрытие stream и соблюдение контекста остаются обязанностью вызывающего кода. Кардинальность procedure ограничена, повторно используемые trace headers заменяются. См. [Telemetry](telemetry.md).

Ревизия Release: `ServerConfig.Validate` отклоняет `MaxHeaderBytes`, при котором внутренний запас net/http (4096 байт) может вызвать целочисленное переполнение. Default и обычные настройки сохранены.
