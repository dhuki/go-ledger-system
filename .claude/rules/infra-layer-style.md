# Infra Layer Style — internal/infra/database/

PostgreSQL data access layer. Each file in `repository/domain/` owns one domain concept. `domainRepository` (defined in `base_domain.go`) is the single receiver for all methods; it holds a `cqrs.DB` interface so transaction-scoped clients can be swapped in transparently.

## File Conventions

| File | Purpose |
|------|---------|
| `base_domain.go` | `DomainRepository` interface + `domainRepository` struct + `NewRepositoryDomain` |
| `transaction.go` | `Transaction()` — begin/commit/rollback wrapper |
| `health_check.go` | DB ping implementation |
| Everything else | One file per domain concept, named after it (`account_balance.go`, `ledger_entry.go`, `transaction_transfer.go`) |

DAO row structs live in `internal/infra/database/dao/` — one file per table.

## Query Patterns

### Read: Return `nil, nil` for Not Found

Never return an error for `sql.ErrNoRows` — not found is a valid result, not a failure.

```go
func (r *domainRepository) GetAccountByID(ctx context.Context, id int64) (*dao.AccountBalances, error) {
    row := r.dbImpl.QueryRowContext(ctx, `SELECT ... FROM account_balances WHERE id = $1`, id)
    var account dao.AccountBalances
    if err := row.Scan(&account.ID, &account.AccountNumber, ...); err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, nil
        }
        logger.Error(ctx, "Failed to GetAccountByID", err)
        return nil, fmt.Errorf("scan account %d: %w", id, err)
    }
    return &account, nil
}
```

- Use `QueryRowContext` for single-row lookups.
- Use `QueryContext` + `defer rows.Close()` + `rows.Err()` check for multi-row.

### Write: Dynamic SET via Pointer Params

All update functions use dynamic SET clause building. Every updatable field in the params struct is a pointer — `nil` means "do not touch this column".

```go
// dao/account_balances.go
type UpdateAccountParams struct {
    ID       int64
    Balance  *int64
    Status   *string
    FrozenAt *time.Time
}
```

```go
func (r *domainRepository) UpdateAccount(ctx context.Context, params dao.UpdateAccountParams) error {
    var setClauses []string
    var args []any

    if params.Balance != nil {
        setClauses = append(setClauses, "balance = $"+strconv.Itoa(len(args)+1))
        args = append(args, *params.Balance)
    }
    if params.Status != nil {
        setClauses = append(setClauses, "status = $"+strconv.Itoa(len(args)+1))
        args = append(args, *params.Status)
    }
    if params.FrozenAt != nil {
        if !params.FrozenAt.IsZero() {
            setClauses = append(setClauses, "frozen_at = $"+strconv.Itoa(len(args)+1))
            args = append(args, *params.FrozenAt)
        } else {
            setClauses = append(setClauses, "frozen_at = NULL")
        }
    }

    // updated_at is always unconditional — no nil check
    setClauses = append(setClauses, "updated_at = NOW()")
    args = append(args, params.ID)

    query := fmt.Sprintf(
        "UPDATE account_balances SET %s WHERE id = $%d",
        strings.Join(setClauses, ", "),
        len(args),
    )

    if _, err := r.dbImpl.ExecContext(ctx, query, args...); err != nil {
        logger.Error(ctx, "Failed to UpdateAccount", err)
        return fmt.Errorf("update account %d: %w", params.ID, err)
    }
    return nil
}
```

### Parameterize Repeated Queries

Never write a separate function per status, type, or filter value. Accept the varying value as a parameter.

```go
// ❌ WRONG — one function per status
func (r *domainRepository) GetActiveAccounts(ctx context.Context) ([]*dao.AccountBalances, error) { ... }
func (r *domainRepository) GetFrozenAccounts(ctx context.Context) ([]*dao.AccountBalances, error) { ... }

// ✅ CORRECT — one parameterized function
func (r *domainRepository) GetAccountsByStatus(ctx context.Context, status string) ([]*dao.AccountBalances, error) {
    rows, err := r.dbImpl.QueryContext(ctx, `SELECT ... FROM account_balances WHERE status = $1`, status)
    ...
}
```

For multiple optional filters, use dynamic WHERE clause building with a params struct defined in `dao/`:

```go
type FilterAccountParams struct {
    Status   *string
    Currency *string
}

func (r *domainRepository) FilterAccounts(ctx context.Context, params dao.FilterAccountParams) ([]*dao.AccountBalances, error) {
    query := `SELECT ... FROM account_balances WHERE 1=1`
    var args []any

    if params.Status != nil {
        args = append(args, *params.Status)
        query += " AND status = $" + strconv.Itoa(len(args))
    }
    if params.Currency != nil {
        args = append(args, *params.Currency)
        query += " AND currency = $" + strconv.Itoa(len(args))
    }
    ...
}
```

### Early Return for Empty Slices

Every bulk function starts with:

```go
if len(items) == 0 {
    return nil
}
```

## CQRS Layer

`internal/infra/database/repository/cqrs/` provides the base `DB` interface used by `domainRepository`. It abstracts `*sql.DB` and `*sql.Tx` so the same domain methods work both inside and outside a transaction — the caller never knows the difference.

## Error Handling

```go
// ✅ CORRECT — "Failed to {FunctionName}" with exact PascalCase function name
if err != nil {
    logger.Error(ctx, "Failed to InsertLedgerEntry", err)
    return fmt.Errorf("insert ledger entry for account %d: %w", accountID, err)
}
```

- Log at `logger.Error` when returning an error.
- Always include identifying context in the wrapped error (`account %d`, `transfer %s`).
- `updated_at` is always appended unconditionally — never guarded by a nil check.

## Database Migrations

Migrations live in `internal/infra/database/repository/domain/migration/`. Every file has exactly **one operation on one table**.

### Naming

`YYYYMMDDHHMMSS_<operation>_<column_or_index>_<table>.sql`

| Operation prefix | Example |
|-----------------|---------|
| `add_column_` | `20260620000001_add_column_currency_account_balances.sql` |
| `drop_column_` | `20260620000002_drop_column_legacy_field_ledger_entries.sql` |
| `add_index_` | `20260620000003_add_index_idx_account_id_ledger_entries.sql` |
| `drop_index_` | `20260620000004_drop_index_idx_old_ledger_entries.sql` |

### Every File Must Have a Down Section

```sql
-- +goose Up
-- +goose StatementBegin
ALTER TABLE account_balances ADD COLUMN currency VARCHAR(10) NOT NULL DEFAULT 'IDR';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE account_balances DROP COLUMN currency;
-- +goose StatementEnd
```

After any migration: update the DAO struct in `dao/<table>.go` and every `row.Scan(...)` that reads the affected table.

## Naming Conventions

### Functions
| Kind | Convention | Example |
|------|-----------|---------|
| Reads | `Get` / `Filter` prefix | `GetAccountByID`, `FilterAccounts` |
| Writes | `Insert` / `Update` / `Delete` prefix | `InsertTransfer`, `UpdateBalance`, `DeleteAccount` |
| Bulk | `Bulk` suffix | `InsertLedgerEntriesBulk` |

## Imports

```go
import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "strconv"
    "strings"

    "github.com/dhuki/go-ledger-system/internal/infra/database/dao"
    "github.com/dhuki/go-ledger-system/internal/infra/logger"
)
```
