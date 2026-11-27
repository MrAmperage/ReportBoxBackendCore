# Static demo

Из корня репозитория:

```sh
STATIC_ROOT=examples/static-demo/public STATIC_SPA=true go run ./examples/static-demo
curl -i http://127.0.0.1:8080/
curl -i -H 'Accept: text/html' http://127.0.0.1:8080/dashboard
curl -i http://127.0.0.1:8080/missing.js
```

Последний запрос возвращает 404, а не HTML. Никаких RPC/Management/DB/NATS подключений не открывается. Подробнее: [Static](../../docs/static.md).
