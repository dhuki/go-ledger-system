# Core Layer Style — internal/core/

Business logic layer. Each subdomain (e.g. `transaction`, `healthcheck`) has its own package with two files:

| File | Purpose |
|------|---------|
| `port.go` | `Service` and `Repository` interface definitions — the dependency boundary |
| `service.go` | `service` struct + `NewService` constructor + all business logic methods |

The `service` struct depends only on the `Repository` interface defined in `port.go` — never on any concrete infra type.

## File Conventions

One subdomain per package. If a subdomain grows large, split into multiple files inside the same package — one file per operation group. Private helpers stay in the same file as the public method that uses them.

## Method Structure

Every service method follows the same shape:

```go
func (s *service) MethodName(ctx context.Context, ...) (..., error) {
    // 1. Fetch data via repository (reads outside transaction)
    // 2. Validate business rules
    // 3. Prepare data for writes (transforms, loops — all outside transaction)
    // 4. Execute writes (single repository call or transaction)
    // 5. Return result
}
```

## Critical Patterns

### Transactions: One Transaction per Multi-Write Process

Any operation that performs more than one database write must wrap all writes in a single transaction. Partial state is never acceptable.

```go
// ❌ WRONG — two writes outside a transaction; second may fail leaving inconsistent data
if err := s.repo.CreateTransfer(ctx, transfer); err != nil {
    return err
}
if err := s.repo.DebitAccount(ctx, sourceID, amount); err != nil {
    return err // transfer already inserted — data is now inconsistent
}

// ✅ CORRECT — both writes are atomic
return s.repo.Transaction(ctx, func(tx domain.DomainRepository) error {
    if err := tx.CreateTransfer(ctx, transfer); err != nil {
        return fmt.Errorf("create transfer: %w", err)
    }
    if err := tx.DebitAccount(ctx, sourceID, amount); err != nil {
        return fmt.Errorf("debit source account: %w", err)
    }
    return nil
})
```

### Transactions: Write-Only

Reads and validation belong **before** the transaction. The transaction block contains only INSERT/UPDATE/DELETE.

```go
// ✅ CORRECT — reads and validation outside, transaction is writes-only
account, err := s.repo.GetAccountByID(ctx, sourceID)
if err != nil {
    logger.Error(ctx, "Failed to GetAccountByID", err)
    return fmt.Errorf("get source account: %w", err)
}
if account.Balance < amount {
    return ErrInsufficientBalance
}

return s.repo.Transaction(ctx, func(tx domain.DomainRepository) error {
    if err := tx.UpdateBalance(ctx, sourceID, -amount); err != nil {
        return fmt.Errorf("update source balance: %w", err)
    }
    if err := tx.InsertLedgerEntry(ctx, entry); err != nil {
        return fmt.Errorf("insert ledger entry: %w", err)
    }
    return nil
})
```

```go
// ❌ WRONG — read inside transaction holds lock unnecessarily
return s.repo.Transaction(ctx, func(tx domain.DomainRepository) error {
    account, err := tx.GetAccountByID(ctx, sourceID) // extends lock duration
    ...
})
```

### SOLID Principles

**S — Single Responsibility**: Public methods orchestrate; private helpers handle specifics. A method that fetches, validates, transforms, and writes is doing too much — split it.

**D — Dependency Inversion**: `service` only imports the `Repository` interface from the same package (`port.go`). It never imports `internal/infra/database` or any concrete implementation. All wiring happens in `cmd/server/main.go`.

```go
// ✅ CORRECT — depends on the interface
type service struct {
    repo Repository // defined in port.go
}

// ❌ WRONG — depends on a concrete implementation
type service struct {
    repo *domain.domainRepository
}
```

**I — Interface Segregation**: `Repository` in `port.go` should only declare what the service actually calls. Do not add methods to the interface speculatively.

### Extract Common Logic

When the same logic appears in more than two service methods, extract it into a private helper. Helpers are always private (lowercase), receive only what they need, and are named as clear actions: `validateTransfer`, `buildLedgerEntry`, `calculateFee`.

### KISS and YAGNI

Write the simplest code that solves the current requirement. Do not add:
- Optional parameters or config flags for hypothetical future callers
- Extra validation for scenarios that cannot currently happen
- Generalized helpers built for a single use case

## Error Handling

```go
// ✅ CORRECT — "Failed to {FunctionName}" with exact PascalCase function name
account, err := s.repo.GetAccountByID(ctx, id)
if err != nil {
    logger.Error(ctx, "Failed to GetAccountByID", err)
    return fmt.Errorf("get account %d: %w", id, err)
}
if account == nil {
    return ErrAccountNotFound
}
```

- Repository returns `nil, nil` for not found — map it to a sentinel error in the service.
- Log at `logger.Error` when returning an error that aborts the operation.
- Log once — do not re-log an error received from the repository.

## Naming Conventions

### Functions
| Kind | Convention | Example |
|------|-----------|---------|
| Public service methods | `VerbNoun` | `CreateTransfer`, `GetAccountBalance`, `HealthCheck` |
| Private helpers | descriptive action | `validateTransfer`, `buildLedgerEntry` |
| Repository interface methods | `VerbNoun` | `GetAccountByID`, `InsertTransfer`, `UpdateBalance` |

## Imports

```go
import (
    "context"
    "fmt"

    "github.com/dhuki/go-ledger-system/internal/infra/database/dao"
    "github.com/dhuki/go-ledger-system/internal/infra/logger"
)
```
