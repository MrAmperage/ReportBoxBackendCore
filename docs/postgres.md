# PostgreSQL и GORM

[Архитектура](architecture.md) · [ADR 0006](adr/postgres.md) · [Пример](../examples/postgres-demo/README.md)

## 1. Подключение

```go
backendcore.Run(app.Info{Name: "reportbox-auth-service"},
    postgres.Module(), orm.Module(), fx.Provide(NewUserRepository))
```

Пакет ORM импортируется как `github.com/MrAmperage/ReportBoxBackendCore/postgres/orm`. `postgres.Module()` принимает ноль или один полный `postgres.Config`: без аргументов читает ENV, явная конфигурация игнорирует module ENV. `orm.Module()` без параметров. ORM без postgres является ошибкой DI. PostgreSQL без ORM работает независимо.

```go
type UserRepository struct { db *gorm.DB }
func NewUserRepository(db *gorm.DB) *UserRepository { return &UserRepository{db: db} }
func (r *UserRepository) ByID(ctx context.Context, id int64) (User, error) {
    return gorm.G[User](r.db).Where("id = ?", id).First(ctx)
}
func (r *UserRepository) Create(ctx context.Context, user *User) error {
    return gorm.G[User](r.db).Create(ctx, user)
}
```

Репозитории получают `*gorm.DB` непосредственно через Fx. В конструкторах только сохраняют зависимость, не выполняют запросы. `db.WithContext(ctx)` для классического API; Generics API принимает context при выполнении. Не мутировать общий `db.Config`, не вызывать `db.DB().Close()`, не менять параметры SQL adapter. Ошибки `gorm.ErrRecordNotFound`, `gorm.ErrDuplicatedKey`, `gorm.ErrForeignKeyViolated` используются через `errors.Is`; TranslateError включён. Внешние ошибки драйвера не передавать пользователю и не логировать без redaction.

Нативный pgx доступен через внедрение `*postgres.Pool` и вызов `Native()`. До Open и после Close accessor возвращает ошибку. Не закрывать возвращённый pool. Для операций `COPY`, PostGIS, массовой записи и аналитики можно использовать pgx непосредственно. Пул принадлежит Core, а не репозиториям. GORM создаёт объект заранее, но gate допускает запросы только после открытия адаптера; `orm.ErrUnavailable` до/после lifecycle. При отменённом context возможна ошибка context вместо ErrUnavailable.

## 2. Конфигурация

| ENV | Default |
|---|---|
| `POSTGRES_DSN` | Обязателен, без default |
| `POSTGRES_ALLOW_PLAINTEXT` | `false` |
| `POSTGRES_MAX_CONNS` | `10` |
| `POSTGRES_MIN_CONNS` | `0` |
| `POSTGRES_CONNECT_TIMEOUT` | `5s` |
| `POSTGRES_HEALTH_TIMEOUT` | `2s` |
| `POSTGRES_MAX_CONN_LIFETIME` | `1h` |
| `POSTGRES_MAX_CONN_IDLE_TIME` | `15m` |
| `POSTGRES_HEALTH_CHECK_PERIOD` | `1m` |

DSN — единственный TCP URL `postgres://` или `postgresql://` с user, database и явным `sslmode=verify-full` либо `sslmode=disable`. Разрешены query options `sslmode`, `sslrootcert`, `sslcert`, `sslkey`; cert/key задаются парой. Использовать percent-encoding для специальных символов пароля и файловых путей. Нативный pgx может читать PG* environment и стандартные файлы credentials; эффективный endpoint и TLS-политика дополнительно проверяются. Не задавать противоречивые PG* настройки. Не разрешены multi-host, unix sockets и произвольные query overrides. `sslmode=verify-full` проверяет доверие и имя сервера; `sslrootcert` — путь к CA либо `system`. В development `disable` допустим для loopback, в production/удалённой сети — только при `POSTGRES_ALLOW_PLAINTEXT=true` и отдельно защищённом транспорте. Не использовать публичный plaintext PostgreSQL. Серверный TLS и реальный mTLS требуют интеграционной проверки. `application_name` задаётся из app.Info.Name, session timezone и GORM NowFunc — UTC.

Пул физически один; database/sql adapter держит 0 idle, MaxOpenConns равен MaxConns pgxpool. Параметры ограничиваются на один процесс: учитывать суммарный бюджет всех реплик. `POSTGRES_HEALTH_TIMEOUT` также ограничивает ping при acquire, если pgx выполняет его. Рабочий deadline запроса задаёт приложение через context, а не ConnectTimeout. Не удерживать соединение/transaction без необходимости.

## 3. Транзакции, миграции и логирование

```go
err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
    return gorm.G[User](tx).Create(ctx, &user)
})
```

Внутри транзакции передавать именно `tx`, не исходный `db`. Для pgx использовать `BeginTx`/`pgx.BeginFunc` и обязательный rollback с ограниченным независимым контекстом при необходимости. GORM/pgx используют общий пул, но не общий transaction context. Нельзя выполнять зависимые запросы через другой пул/адаптер внутри той же транзакции и считать их атомарными; можно получить deadlock при исчерпании пула. Не выполнять автоматические retry при неопределённом исходе commit. Transactional Outbox — отдельная задача.

Миграции принадлежат сервису; ядро не вызывает `AutoMigrate`. Для production использовать версионированные миграции до запуска реплик. Инструмент миграций ещё не выбран. `safeLogger` интегрирует GORM с slog: error без raw текста и SQL, slow query warning после 200ms, без вызова SQL formatter; GORM level Warn по умолчанию. Уровень основного slog тоже действует. Это намеренно ограничивает диагностику; будущий opt-in безопасный telemetry требует отдельного решения. Не заменять logger на небезопасный production logger.

## 4. Lifecycle

`postgres.pool` открывает pgxpool, выполняет startup Ping, регистрирует required probe. `postgres.orm` зависит от `postgres.pool`, открывает `database/sql` adapter и публикует его в gate. Пользовательские lifecycle participants, использующие БД в Open/Activate/Close, явно указывают `DependsOn: []string{postgres.ParticipantName}` или `orm.ParticipantName`; одного DI недостаточно для зависимости lifecycle. В стандартных RPC/NATS handlers допускается использование БД после Running; все Drain завершаются до Close. ORM закрывается перед pool. Close отключает accessor/gate и ожидает завершения закрытия в пределах общего deadline. При timeout `pgxpool.Close()` может продолжать ждать чужой Release/Rollback; не обещать force-close. Владелец acquired conn, rows и transactions обязан их освобождать. Core не открывает БД при отсутствии модуля.

## 5. Проверки

Интеграционные тесты с `TEST_POSTGRES_DSN`: native pool startup/health/stop; GORM CRUD и duplicate mapping, rollback, исчерпание единого пула и early/late access. Тесты используют случайную таблицу в изолированной БД, не запускать на production. Без переменной интеграционные тесты пропускаются. Unit tests config/policy и gate прошли локально с Go 1.23.2 и race detector (`GO111MODULE=off` для изолированных файлов); их vet тоже прошёл. GORM logger test и Fx/driver/integration tests не запускались. Go 1.27 download через Go proxy заблокирован DNS; `go mod tidy` не выполнен. Checksums взяты из опубликованных go.sum (pgx и GORM upstream и потребители); необходимо нормализовать и проверить их через Go toolchain. Перед merge выполнить весь checklist [development](development.md), отдельно реальные TLS/mTLS, отказ при недоступной БД и shutdown с активной транзакцией. Кандидат не является проверенным релизом.

Источники API: [pgxpool](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool), [OpenDBFromPool](https://pkg.go.dev/github.com/jackc/pgx/v5/stdlib#OpenDBFromPool), [GORM](https://gorm.io/docs/), [pgx changelog](https://github.com/jackc/pgx/blob/v5.13.0/CHANGELOG.md).
