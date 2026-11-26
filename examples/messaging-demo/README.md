# Пример NATS / JetStream

Только для выделенного локального брокера. Docker и Go 1.27 требуются отдельно. Публикации demo не являются бизнес-событиями.

```sh
docker run --rm --name reportbox-nats-demo -p 127.0.0.1:4222:4222 nats:2.11-alpine -js
```

Из корня репозитория в разных терминалах:

```sh
go run ./examples/messaging-demo -mode core-worker
go run ./examples/messaging-demo -mode core-publish -message hello
```

Для JetStream предварительно установить [NATS CLI](https://github.com/nats-io/natscli) и создать stream / consumer на этом локальном брокере. Никаких автоматических изменений production-конфигурации:

```sh
nats --server nats://127.0.0.1:4222 stream add REPORTBOX_DEMO --subjects 'reportbox.demo.event.v1' --storage memory --retention limits --max-age 1h --max-bytes 1048576 --defaults
nats --server nats://127.0.0.1:4222 consumer add REPORTBOX_DEMO demo-worker --pull --ack explicit --wait 15s --max-deliver 5 --max-pending 32 --deliver all --replay instant --defaults
```

```sh
go run ./examples/messaging-demo -mode js-worker
go run ./examples/messaging-demo -mode js-publish -id demo-1 -message hello
```

Повторная публикация с `-id demo-1` в окне дедупликации stream не создаёт вторую запись. Core NATS и JetStream используют разные subjects. Вызовы CLI могут отличаться между версиями; при необходимости проверить `nats stream add --help` и `nats consumer add --help`. Остановить worker через Ctrl+C, затем локальный контейнер. Не подключать этот пример к production.

Интеграционные тесты сами создают временные stream/consumer, используют уникальные имена и удаляют ресурсы после теста:

```sh
NATS_TEST_URL=nats://127.0.0.1:4222 go test -race -count=1 ./nats ./jetstream
```

См. [API и настройки](../../docs/nats.md).
