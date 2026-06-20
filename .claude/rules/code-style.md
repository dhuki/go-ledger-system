# Code Style — All Layers

Implementation rules that apply across every layer. Layer-specific rules live in `core-layer-style.md` (business logic) and `infra-layer-style.md` (database/repository).

These guidelines are **continuously evolving** — update the relevant rule file whenever a new pattern is established, an existing rule is refined, or a better approach is discovered during development.

## Context Propagation

`context.Context` must be the **first parameter** of every function across all layers — adapter, core, and infra. The context originates from the incoming HTTP request in the adapter layer and propagates unchanged through every call.

```go
// ✅ CORRECT — ctx flows from router → handler → service → repository
func (h *transactionHandler) CreateTransfer(c *gin.Context) {
    ctx := c.Request.Context() // origin: the HTTP request context
    result, err := h.service.CreateTransfer(ctx, req)
    ...
}

func (s *service) CreateTransfer(ctx context.Context, req TransferRequest) error {
    return s.repo.InsertTransfer(ctx, params)
}

func (r *domainRepository) InsertTransfer(ctx context.Context, params dao.TransactionTransfers) error {
    _, err := r.dbImpl.ExecContext(ctx, query, params.SourceAccountID, ...)
    ...
}
```

```go
// ❌ WRONG — context dropped, background context created mid-call
func (s *service) CreateTransfer(req TransferRequest) error {
    ctx := context.Background() // loses trace ID, cancellation, deadlines
    s.repo.InsertTransfer(ctx, params)
}
```

**Rules:**
- Adapter (`internal/adapter/`) takes ctx from `c.Request.Context()` — never create one.
- Core (`internal/core/`) and infra (`internal/infra/`) always forward the received ctx — never create one mid-call.
- Background workers may use `context.Background()` only at their own top-level goroutine entry point.

## Data Ownership

**`internal/infra/database/dao/` is the only place where database row structs may be defined.** Core and adapter layers import from there — they never declare their own DB structs.

| What | Location |
|------|----------|
| Database row structs | `internal/infra/database/dao/*.go` |
| Sentinel errors | defined in the relevant `core` package |
| Request/response types | defined in the relevant `core` package (port.go or types.go) |

```go
// ❌ WRONG — struct defined inside core or adapter file
type transferRow struct { ... }

// ✅ CORRECT — defined in dao/, imported everywhere
import "github.com/dhuki/go-ledger-system/internal/infra/database/dao"

func (r *domainRepository) InsertTransfer(ctx context.Context, row dao.TransactionTransfers) error { ... }
```

## Error Handling

- Repository returns `nil, nil` for "not found" — this is not an error.
- Core service maps `nil` result to a sentinel error defined in the same package.
- Always wrap errors with context: `fmt.Errorf("brief context: %w", err)`.

### Error Logging

**Every error that causes a function to return early must be logged before returning.** Use the matching logger level based on severity. Only log the error and a brief description of what was being attempted — no extra fields.

```go
// ✅ CORRECT — logger.{level}(ctx, "Failed to {FunctionName}", err)
if err != nil {
    logger.Error(ctx, "Failed to InsertTransfer", err)
    return fmt.Errorf("insert transfer: %w", err)
}
```

```go
// ❌ WRONG — error returned silently, no log
if err != nil {
    return fmt.Errorf("insert transfer: %w", err)
}

// ❌ WRONG — freeform sentence instead of "Failed to {FunctionName}"
if err != nil {
    logger.Error(ctx, "failed to insert transfer record into database for source account", err)
    return fmt.Errorf(...)
}

// ❌ WRONG — lowercase function name
if err != nil {
    logger.Error(ctx, "Failed to insertTransfer", err)
    return fmt.Errorf(...)
}
```

**Log level guide:**

| Level | When to use |
|-------|-------------|
| `logger.Error` | Error aborts the operation and propagates up |
| `logger.Warn` | Best-effort side effect failed but did not abort |
| `logger.Info` | Significant operation started or completed successfully |
| `logger.Fatal` | Unrecoverable startup failure (e.g. DB connection failed) |
| `logger.Debug` | Diagnostic detail useful during development only |

**Rules:**
- Message format is always `"Failed to {FunctionName}"` — use the exact function name being called, in `PascalCase`.
- Log once at the point of first handling — do not re-log the same error at every layer.
- No extra fields beside the error — context is carried by the wrapped error message and the trace ID in `ctx`.

### Custom Errors

Sentinel errors are defined as package-level variables using `errors.New`. Never hardcode error strings inline.

```go
// ❌ WRONG — hardcoded inline
return fmt.Errorf("account not found")
return errors.New("insufficient balance")

// ✅ CORRECT — defined once in the package, imported by callers
var (
    ErrAccountNotFound    = errors.New("account not found")
    ErrInsufficientBalance = errors.New("insufficient balance")
)

return ErrAccountNotFound
```

Callers check with `errors.Is`. Wrapping for context is allowed — the sentinel still lives in the package:
```go
return fmt.Errorf("create transfer for account %d: %w", id, ErrInsufficientBalance)
```

## File & Folder Naming

- **`.go` files**: `snake_case` — named after the primary entity or operation (`account_balance.go`, `transaction_transfer.go`, `base_cqrs.go`)
- **Folders**: lowercase single word, no spaces, no underscores (`handler`, `middleware`, `cqrs`, `domain`)

## Naming Conventions

### Types
| Kind | Convention | Example |
|------|-----------|---------|
| Structs / DAO row types | `PascalCase` | `AccountBalances`, `LedgerEntries`, `TransactionTransfers` |
| Params / filter structs | `PascalCase` + `Params` suffix | `UpdateBalanceParams`, `FilterTransferParams` |
| Sentinel errors | `Err` prefix | `ErrAccountNotFound`, `ErrInsufficientBalance` |

### Functions
| Kind | Convention | Example |
|------|-----------|---------|
| Constructors | `New` prefix | `NewService`, `NewTransactionHandler` |
| Utility / builders | `Build` / `Convert` prefix | `BuildLedgerEntry`, `ConvertToResponse` |

### Receivers & Variables
- Receivers: short abbreviation — `s` for service, `r` for repository/domainRepository, `h` for handler
- Local variables: `camelCase`, descriptive — `sourceAccount`, `ledgerEntry`
- Struct fields: `PascalCase` (exported)
