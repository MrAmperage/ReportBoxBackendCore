# Auth — проверка токенов и актуальности полномочий

[Архитектура](architecture.md) · [ADR 0011](adr/auth.md) · [RPC](rpc.md)

`auth` — опциональный, пассивный модуль проверки подписанных Access Token. Он не выпускает токены, не хранит сессии, не содержит моделей пользователей, организаций, ролей или разрешений. Не открывает соединений, listener или goroutines. Не требует RPC, NATS, PostgreSQL или Telemetry. Сервис предоставляет два доверенных интерфейса; именно они реализуют взаимодействие с источником истины, кеш и синхронизацию. Проверка состояния обязательна даже при корректной подписи.

## 1. Конфигурация и DI

| ENV | Default | Назначение |
|---|---|---|
| `AUTH_ISSUER` | обязательный | Один доверенный issuer, точное сравнение |
| `AUTH_AUDIENCE` | обязательный | Требуемая audience (допускается в массиве) |
| `AUTH_ALGORITHM` | `EdDSA` | Ровно один из `EdDSA`, `RS256`, `ES256` |
| `AUTH_TOKEN_TYPE` | `at+jwt` | Точный JOSE `typ`, отдельный от refresh token |
| `AUTH_CLOCK_SKEW` | `30s` | Допуск расхождения часов, 0–60s |
| `AUTH_PROVIDER_TIMEOUT` | `2s` | На каждый вызов провайдера, >0 и ≤60s |
| `AUTH_MAX_TOKEN_BYTES` | `8192` | Максимальная длина compact JWT, 256–65536 |

`auth.Module()` читает ENV; `auth.Module(cfg)` принимает один полный `Config`, не читая ENV модуля. `DefaultConfig`, `LoadConfig`, `Config.Validate` не выполняют сетевой I/O. `auth.NewValidator(cfg, keys, state)` доступен без Fx. Fx `Module` требует явных bindings `KeyProvider`, `StateValidator` и создаёт `*auth.Validator` при построении графа, даже если его не запросили другие компоненты. Ошибка конфигурации, отсутствующий или typed-nil provider приводят к ошибке построения. У модуля нет собственного lifecycle participant: владельцы сетевых ресурсов, фоновой синхронизации и readiness — провайдеры потребителя. Регистрировать их lifecycle и проверки готовности по [Core](core.md). Не делать RPC в DI constructor.

```go
cfg := auth.DefaultConfig()
cfg.Issuer = "reportbox-auth"
cfg.Audience = "reportbox-transport"
// provider — собственный клиент, реализующий оба интерфейса.
backendcore.Run(info,
    auth.Module(cfg),
    fx.Provide(
        func(p *AuthenticationClient) auth.KeyProvider { return p },
        func(p *AuthenticationClient) auth.StateValidator { return p },
    ),
    // Остальные модули и конструкторы конкретного сервиса.
)
```

## 2. Контракт токена

Compact JWS из трёх непустых base64url сегментов без padding. JWT library `github.com/golang-jwt/jwt/v5` проверяет подпись и стандартные claims; дополнительная строгая проверка структуры исключает неоднозначную интерпретацию JSON и ограничивает объём работы. `alg`, `typ`, `kid` обязательны и являются единственными разрешёнными JOSE headers. Не поддерживаются `none`, HMAC, JWE, embedded JWK, `jku`, `x5u`, `x5c`, `crit` и другие расширения. `kid` — 1–128 ASCII символов `[A-Za-z0-9._-]`, кроме `.` и `..`; это идентификатор, не путь или URL. Ключи выбираются только из доверенного источника по конфигурации, а не по адресам, переданным токеном. Неподдерживаемый формат отклоняется до обращения к провайдеру.

Обязательные claims: `iss`, `sub`, `aud`, `exp`. `iat`, `nbf`, `jti` опциональны, но проверяются при наличии; пустые или null значения не принимаются. NumericDate — целые Unix seconds в диапазоне 1970–9999, не дробные/строковые значения; expiry должен быть позже присутствующих iat/nbf. Audience — строка или массив непустых уникальных строк (до 32). JSON дубликаты (включая вложенные), неправильный UTF-8, trailing JSON и чрезмерная вложенность отклоняются. `exp` учитывает настроенный clock skew; после `exp + skew` токен не принимается. Время жизни и набор обязательных прикладных claims определяет issuer/state provider, а не Core. Использовать короткоживущие Access Token, отдельный тип и отдельный контракт Refresh Token. Для ReportBox рекомендован Ed25519; другие алгоритмы нужны для совместимости. RSA ключи 2048–8192 бит с допустимым нечётным exponent не меньше 65537, ES256 — только P-256, EdDSA — Ed25519 public key.

## 3. Интерфейсы провайдеров

```go
type KeyProvider interface {
    ResolveKey(ctx context.Context, issuer, keyID, algorithm string) (crypto.PublicKey, error)
}

type StateValidator interface {
    Validate(ctx context.Context, identity Identity, mode ValidationMode) error
}
```

Оба провайдера должны быть concurrency-safe, учитывать deadline/cancellation и не раскрывать секреты в ошибках/логах. Core не создаёт goroutine для принудительного прерывания: некооперативный provider может блокировать вызов до собственного возврата. Положительный результат, полученный после отмены/таймаута, не используется. Timeout не увеличивает deadline исходного запроса. Сначала выполняется проверка формата, issuer/audience/type/сроков, затем получение ключа, затем криптографическая проверка, затем обязательный state check. Неверифицированные данные не передаются StateValidator; KeyProvider получает только фиксированный issuer/algorithm и ограниченный `kid` (kid ещё не подтверждён подписью). Провайдер обязан ограничивать кеш и параллельные refresh, не допускать лавины запросов при произвольных неизвестных kid. Не мутировать возвращённые публичные ключи.

### Актуальность полномочий

`LocalPreferred` — использовать достоверное, достаточно свежее локальное состояние либо выполнить авторитетный удалённый запрос; при неизвестном результате — ошибка.

`RemoteRequired` — проверка в авторитетном источнике для каждого вызова, без положительного ответа из локального кеша. Модуль передаёт режим, но не может проверить, действительно ли внешний provider выполнил RPC. Соблюдение режима — контракт реализации provider. Core не предоставляет permissive/skip-state mode. Ни один режим не разрешает обход проверки подписи.

StateValidator проверяет сессию, актуальность субъекта, ключа, ревизий полномочий и обязательных прикладных claims. Он получает проверенную Identity, не raw JWT; удалённый протокол должен аутентифицировать вызывающий сервис и валидировать данные по авторитетному состоянию, не доверять произвольным пользовательским параметрам. Список/модель прав и версия сессии — соглашение AuthenticationService/AuthenticationClient. Не принимать nil при неопределённом состоянии или неизвестной ревизии.

Для ReportBox AuthenticationClient должен реализовать ограниченный TTL доверия состоянию, удалённую проверку при cache miss или потере синхронизации, начальное восстановление и gap/reconnect recovery, монотонное применение изменений, раздельную доставку invalidation каждому экземпляру сервиса. Изменения в БД и события требуют transactional outbox (не часть auth); изменения общей роли должны инвалидировать всех затронутых субъектов. Подключение к брокеру само по себе не доказывает свежесть состояния. Никаких гарантий мгновенной инвалидации при локальном кеше; окно отзыва зависит от реализации и TTL. `RemoteRequired` даёт решение по состоянию на момент проверки, а не атомарность с последующей бизнес-транзакцией. Строгое ограничение бизнес-эффектов требует прикладной проверки/согласования на границе операции. Отказ источника без достоверного локального состояния — fail closed.

## 4. Identity и ошибки

```go
identity, err := validator.Validate(ctx, rawAccessToken, auth.LocalPreferred)
if err != nil { /* отказ */ }
ctx = auth.WithIdentity(ctx, identity)
identity, ok := auth.FromContext(ctx)
```

`Identity` содержит приватные поля; доступны `Issuer()`, `Subject()`, `Audience()`, `KeyID()`, `Algorithm()`, `TokenType()`, `TokenID()`, `ExpiresAt()`, `IssuedAt()`, `NotBefore()` и `Claim(name) (json.RawMessage, bool)`. Audience и raw claim возвращаются копиями; числовые claims декодировать в целочисленный тип или `json.Number`, а не float64. Токен целиком не хранится. `WithIdentity` принимает только identity, полученную доверенным кодом, zero value очищает её; `FromContext` не проверяет актуальность повторно. Не логировать claims, bearer credentials или идентификаторы сессий. Обычное форматирование Identity скрывает содержимое.

| Ошибка Core | Значение | ConnectRPC |
|---|---|---|
| `ErrInvalidToken` | Нет/неверный/истёкший токен, неизвестный signing key | `Unauthenticated` |
| `ErrUnknownKey` | Только provider: авторитетное отсутствие key | `Unauthenticated` |
| `ErrRevoked` | Отзыв/устаревшая ревизия | `Unauthenticated` |
| `ErrPermissionDenied` | Недостаточно прав | `PermissionDenied` |
| `ErrUnavailable` | Неизвестная актуальность, provider timeout/error, неверный ключ provider | `Unavailable` |
| `context.Canceled` / `DeadlineExceeded` | Отмена/срок исходного запроса | `Canceled` / `DeadlineExceeded` |

Не передаются произвольные error strings провайдера. Ненулевой режим вне известных и недействительный конфиг не разрешают доступ. `ErrUnknownKey` не использовать для временно недоступного источника ключей. При ошибке Validate возвращает zero Identity.

## 5. Интеграция с RPC

`auth/rpc` (имя пакета `authrpc`) предоставляет `NewAuthorize(*auth.Validator, map[string]Policy)`; результат прямо присваивается существующему `rpc.Handler.Authorize`. Правила копируются при построении. Ключи — полные canonical Protobuf procedure names; wildcard не поддерживается. Неуказанные методы отклоняются. Нулевая `Policy{}` требует JWT и `LocalPreferred`. `Mode: auth.RemoteRequired` для операций с повышенным требованием актуальности. `Check` — опциональная прикладная проверка прав с доступом к подтверждённой Identity; возвращать `auth.ErrPermissionDenied` для отказа. `AllowUnauthenticated: true` допускается только явно, без Check и без RemoteRequired; предыдущая Identity в контексте очищается. Ресурсные права (доступ к конкретному предприятию, транспорту и т. п.) проверяются в обработчике, где доступны данные запроса. Авторизованный субъект не означает автоматического доступа ко всем объектам.

```go
// В конструкторе rpc.Handler:
authorize, err := authrpc.NewAuthorize(v, map[string]authrpc.Policy{
    "/reportbox.example.v1.Service/Read": {},
    "/reportbox.example.v1.Service/Write": {
        Mode: auth.RemoteRequired,
        Check: checkWritePermission,
    },
})
if err != nil { return rpc.Handler{}, err }
return rpc.Handler{Build: buildHandler, Authorize: authorize}, nil
```

Bearer token передаётся только одним заголовком Authorization; повторные/неоднозначные значения отклоняются. Исходный context, deadline и request ID сохраняются. `authrpc` не меняет публичный API `rpc`. Вызывать `Authorize` для каждого unary RPC. У streaming RPC существующий RPC hook вызывается только при открытии. Для длительных потоков и фоновых действий использовать `validator.Revalidate(ctx, identity, mode)` перед защищёнными действиями или в контролируемом цикле; метод проверяет время и состояние, принимает только Identity того же Validator. Не проверяет подпись заново, поэтому отзыв key обязан учитывать StateValidator. При ошибке завершать поток. Автоматическое завершение уже открытых streams или отмена начатой операции не реализованы; при необходимости прикладной сервис реализует собственную политику. Не переносить bearer token автоматически между сервисами.

## 6. Проверки

`go run ./examples/auth-demo` — локальный пример с эфемерным ключом и исключительно демонстрационным provider. `go test -race ./auth/...` — unit-тесты структуры токена, конфигурации, keys, signatures, состояния, Fx, RPC policy и реальный RPC listener integration. Полный release gate — [release.md](release.md). Проверка провайдеров AuthenticationClient, ротации ключей, доставки invalidations и freshness guarantees должна выполняться в прикладном проекте.
