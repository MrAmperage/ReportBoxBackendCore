# RPC demo

Два процесса, один контракт Protobuf (`echo.proto`), без Caddy и БД. Для простоты используются уже сгенерированные Protobuf-типы `wrapperspb.StringValue` и нативные типизированные `connect.NewUnaryHandler`/`connect.NewClient`, а не сгенерированные service stubs. В прикладных сервисах предпочтительна генерация из `.proto`.

Терминал 1:

```sh
go run ./examples/rpc-demo -mode server
```

Терминал 2:

```sh
go run ./examples/rpc-demo -mode client -message 'Hello ReportBox'
```

По умолчанию loopback и gRPC поверх h2c. Для Connect JSON можно выполнить:

```sh
curl -X POST http://127.0.0.1:9090/reportbox.example.v1.EchoService/Echo \
  -H 'Content-Type: application/json' -H 'Connect-Protocol-Version: 1' \
  -d '"Hello ReportBox"'
```

Echo явно разрешён без авторизации только в демонстрационном сервисе. Не публиковать его в production. Пример не меняет Caddyfile или NaiveProxy. См. [документацию RPC](../../docs/rpc.md).
