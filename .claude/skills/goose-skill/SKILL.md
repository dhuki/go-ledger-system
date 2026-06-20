---
name: goose-skill
description: >
  Creates goose SQL migration files for this project and places them in
  internal/infra/database/repository/domain/migration/. Use this skill
  whenever the user asks to create a migration, add a table, add a column,
  drop a column, create an index, or make any schema change — even if they
  don't say "goose" or "migration" explicitly. Trigger on phrases like
  "create migration", "add table", "new migration", "migrate", "alter table",
  "add column", "drop column", "create index", or any schema-change request.
---

# Goose Migration Skill

This project uses [goose v3](https://github.com/pressly/goose) with a **PostgreSQL** dialect.
Migrations live in:

```
internal/infra/database/repository/domain/migration/
```

Goose auto-runs `Up` on startup via `goose.Up(db, migrationDir)` in `internal/infra/database/psql.go`.

---

## How to create a migration

### 1. Derive the filename

Goose expects: `{YYYYMMDDHHmmss}_{descriptive_name}.sql`

Use the current timestamp (UTC) and a snake_case name describing the change.

Examples:
- `20240620143000_create_account_balances_table.sql`
- `20240620143100_add_status_to_transaction_transfers.sql`
- `20240620143200_create_index_on_ledger_entries_transaction_id.sql`

### 2. Write the file

Every SQL migration file must have `-- +goose Up` (required) and `-- +goose Down` (strongly recommended for rollback safety).

**Template:**
```sql
-- +goose Up
-- write your forward migration SQL here

-- +goose Down
-- write your rollback SQL here
```

### 3. Place the file

Always write the migration file to:
```
internal/infra/database/repository/domain/migration/{timestamp}_{name}.sql
```

---

## SQL rules for this project

- **Database**: PostgreSQL — use PostgreSQL syntax (e.g. `SERIAL` or `BIGSERIAL`, `TEXT`, `TIMESTAMPTZ`, `BOOLEAN`).
- **Primary keys**: use `BIGSERIAL PRIMARY KEY` for auto-increment integer PKs.
- **Timestamps**: always use `TIMESTAMPTZ` (timezone-aware), never `TIMESTAMP`.
- **Amounts / money**: `BIGINT` (stores smallest denomination, e.g. cents). Never `DECIMAL` or `FLOAT`.
- **Status / type columns**: `VARCHAR(50) NOT NULL` with a `CHECK` constraint listing valid values.
- **Audit columns**: every table should have `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()` and `updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`.
- **Indexes**: add a separate `CREATE INDEX` statement in the same `-- +goose Up` block rather than inline on the column.
- **Foreign keys**: declare them explicitly with `REFERENCES` and `ON DELETE` behaviour.
- **Complex statements** (functions, triggers, DO blocks): wrap with `-- +goose StatementBegin` / `-- +goose StatementEnd`.
- **Non-transactional DDL** (e.g. `CREATE INDEX CONCURRENTLY`): add `-- +goose NO TRANSACTION` at the top of the file.

---

## Examples

### Create a table

```sql
-- +goose Up
CREATE TABLE account_balances (
    id             BIGSERIAL    PRIMARY KEY,
    account_number VARCHAR(50)  NOT NULL UNIQUE,
    holder_name    TEXT         NOT NULL,
    balance        BIGINT       NOT NULL DEFAULT 0,
    currency       VARCHAR(10)  NOT NULL DEFAULT 'IDR',
    status         VARCHAR(50)  NOT NULL DEFAULT 'ACTIVE'
                   CHECK (status IN ('ACTIVE', 'FROZEN', 'CLOSED')),
    frozen_at      TIMESTAMPTZ,
    closed_at      TIMESTAMPTZ,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_account_balances_account_number ON account_balances (account_number);

-- +goose Down
DROP TABLE IF EXISTS account_balances;
```

### Add a column

```sql
-- +goose Up
ALTER TABLE transaction_transfers
    ADD COLUMN retry_count INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE transaction_transfers
    DROP COLUMN retry_count;
```

### Create an index concurrently (non-transactional)

```sql
-- +goose NO TRANSACTION

-- +goose Up
CREATE INDEX CONCURRENTLY idx_ledger_entries_account_id
    ON ledger_entries (account_id);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS idx_ledger_entries_account_id;
```

---

## Checklist before writing the file

1. Is the filename timestamp-based and snake_case?
2. Does `-- +goose Up` appear before `-- +goose Down`?
3. Are all timestamps `TIMESTAMPTZ`?
4. Are money amounts `BIGINT`?
5. Does every new table have `created_at` and `updated_at`?
6. Is the `-- +goose Down` block a correct and safe rollback of everything in `Up`?
