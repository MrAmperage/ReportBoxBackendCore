# Auth demo

```bash
go run ./examples/auth-demo
```

Демонстрация Fx, Ed25519, проверки JWT, state provider, `authrpc.NewAuthorize` и Identity из context. Генерируется эфемерная пара ключей только для запуска примера. Токен не выводится, сервер не запускается. Провайдер принимает только демонстрационного пользователя с известным ключом, `RemoteRequired` отвергается (удалённого источника нет). Не использовать этот provider в реальных сервисах. Реальный RPC listener проверяется в `auth/rpc/integration_test.go`.

[Auth API](../../docs/auth.md) · [RPC](../../docs/rpc.md)
