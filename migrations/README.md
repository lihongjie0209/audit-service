# Migrations

Database-specific migrations live under `mysql`, `postgres`, and `kingbase`. Set `migration.path` to the matching directory, for example `migrations/postgres`. Review indexes, collation and online-DDL impact against production data before deployment.

`audit_records` and its global idempotency key table are append-only. Database
triggers derive actor, timestamp and version fields from the transaction session
and reject row updates or deletes. PostgreSQL retention archives and then drops
monthly partitions; Kingbase/MySQL retention requires an explicitly reviewed
maintenance migration after archive verification. Runtime credentials cannot
perform retention deletion.
