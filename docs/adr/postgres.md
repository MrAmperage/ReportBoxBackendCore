# ADR 0006 — PostgreSQL и GORM

| Решение | Мотивация |
|---|---|
| GORM Generics API для прикладных репозиториев | Не писать ручной SQL/Scan для обычного CRUD и связей. |
| `postgres.Module()` + опциональный `orm.Module()` | Один pgxpool на сервис, прямой доступ к pgx для специализированных операций. |
| `stdlib.OpenDBFromPool`, `MaxIdleConns=0` | Не удерживать соединения в database/sql и не создавать независимый физический пул. |
| `*gorm.DB` стабилен в DI, gate до Open | Конструкторы не открывают сетевые ресурсы; ранние/поздние запросы возвращают ошибку. |
| Отдельные транзакции pgx и GORM | Общий пул не делает транзакции общими; используем нативные API. |
| Без AutoMigrate и BaseRepository | Схемой и доменными запросами владеет сервис. |
| TLS verify-full (TLS >= 1.2) или явный plaintext | Не использовать silent fallback или непроверенный TLS. |
| Явный порт, запрет конкурирующих PG* settings | Не допускать скрытых изменений endpoint, session policy и timeout. Credentials и trust files остаются допустимыми в рамках политики. |
| Cleanup в оставшемся startup budget | Истечение connect timeout не должно автоматически отменять ожидание закрытия частично созданного пула. |
| Логгер не выводит SQL, параметры и raw errors | Исключить утечку приватных данных, включая SQL literals. |
| UTC session и GORM timestamps | Единое хранение времени; преобразование в UI отдельно. |

Ограничения: нет migration runner, outbox, multi-DB, pool failover, гарантированного force-close чужих транзакций, автоматического retry бизнес-операций. `PGSERVICE`, `PGOPTIONS`, `PGCONNECT_TIMEOUT` отклоняются при непустом значении; `PGSSLCERTMODE=disable` несовместим с явно указанным клиентским сертификатом. Неявный порт нормализуется к 5432. См. [API PostgreSQL](../postgres.md).
