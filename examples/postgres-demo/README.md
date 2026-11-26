# PostgreSQL demo

Пример внедрения `*gorm.DB` через Fx и Generics API. Приложение не создаёт таблицу автоматически. Для локальной проверки поднимите отдельную тестовую БД:

```sh
docker run --rm -d --name reportbox-postgres-test \
  -e POSTGRES_USER=reportbox -e POSTGRES_PASSWORD=localtest \
  -e POSTGRES_DB=reportbox -p 127.0.0.1:55432:5432 postgres:17-alpine
export TEST_POSTGRES_DSN='postgres://reportbox:localtest@127.0.0.1:55432/reportbox?sslmode=disable'
until docker exec reportbox-postgres-test pg_isready -U reportbox -d reportbox; do sleep 1; done

go mod tidy
go test -race -count=1 ./postgres/...
```

Отдельно создайте демонстрационную таблицу (только в тестовой БД):

```sh
docker exec reportbox-postgres-test psql -U reportbox -d reportbox -c \
  'CREATE TABLE notes (id bigint PRIMARY KEY, text text NOT NULL); INSERT INTO notes VALUES (1, '\''test'\'');'
POSTGRES_DSN="$TEST_POSTGRES_DSN" go run ./examples/postgres-demo
docker rm -f reportbox-postgres-test
```

Интеграционные тесты используют изолированные таблицы, не требуют demo schema. Для production не использовать эти credentials/настройки.
