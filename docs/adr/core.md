# ADR 0002 — Core и координация lifecycle

[Базовый ADR](baseline.md) · [Документация Core](../core.md)

| Решение | Причина |
|---|---|
| Go и Uber Fx, версии в `go.mod` | Один воспроизводимый module graph. |
| `New`, `Run`, `NewWithConfig`, `NewWithOverrides` | Разрешение таймаутов до Fx, явное управление конфигурацией без повторной регистрации DI. |
| `lifecycle.Participant` в Fx value group | Одна декларация участника, детерминированный порядок, общий phased shutdown. |
| Один внутренний coordinator, без глобального состояния | Изоляция приложений, возможность проверять lifecycle без Fx. |
| `health.Registry` с последовательными probes, общий бюджет 2s | Нет неограниченных goroutines; required checks влияют на readiness, optional нет. |
| Резерв 20% startup deadline (максимум 5s и StopTimeout) | Время для rollback в общем deadline. |
| `lifecycle.ReportFailure` | Снять readiness и запросить nonzero exit при runtime failure. |
| Recovery в `coordinator.call` | Panic callback превращается в безопасную ошибку с фазой и именем; rollback/cleanup остальных участников продолжаются. |

Ограничения: экземпляр приложения одноразовый. Прямые Fx hooks не получают гарантий координатора. Hooks и checks должны сотрудничать с отменой контекста; Open/Activate обязаны очищать собственные частично созданные ресурсы даже при panic. Core сам не открывает порты. Ошибки callback должны быть безопасными для логов. См. [процедуру проверки](../release.md).
