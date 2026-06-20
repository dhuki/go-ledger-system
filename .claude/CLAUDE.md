# go-ledger-system — Project Overview

Ledger microservice handling double-entry bookkeeping, account balance management, and fund transfers between accounts.

## Tech Stack

- **Language**: Go
- **Transport**: REST (Gin)
- **Database**: PostgreSQL (raw `database/sql`, migrations via goose)
- **Logging**: Logrus + Elastic APM
- **Architecture**: Clean/Hexagonal — strict dependency inversion

## Project Structure

```
cmd/server/             Entry point — wires all dependencies
internal/
  adapter/
    http/
      handler/          HTTP handlers — thin, no business logic
      middleware/       Gin middlewares (trace ID injection, request logging)
      v1/               Router setup and route registration
  core/
    healthcheck/        Health check service and port definitions
    transaction/        Transaction/transfer service and port definitions
  infra/
    configloader/       Env config loader (.env + OS env with defaults)
    database/
      dao/              Database row structs (AccountBalances, LedgerEntries, TransactionTransfers)
      repository/
        cqrs/           Base CQRS query/command abstraction
        domain/         Domain repository implementations
      psql.go           PostgreSQL client setup (connection pool, ping, migrations)
    logger/             Logrus-based structured JSON logger with APM integration
utils/
  server.go             GracefulShutdown helper
```

## Important Commands

```bash
# Run the service
go run ./cmd/server/main.go

# Run all tests
go test ./...

# Run a single test
go test ./internal/core/... -run TestFunctionName -v

# Run tests with coverage
go test -cover ./...

# Run tests with race detector
go test -race ./...

# Docker (from project root)
make up         # build and start all services
make down       # stop all services
make restart    # down + up
make build      # build binary to ./bin/go-ledger-system

# Database migrations (using goose)
goose -dir internal/infra/database/repository/domain/migration postgres "host=localhost port=5432 dbname=ledger user=ledger password= sslmode=disable" up
goose -dir internal/infra/database/repository/domain/migration postgres "..." down
```

## Architecture

Ports & Adapters (Hexagonal) — the core defines what it needs via interfaces (ports); adapters implement or consume those interfaces from the outside.

```
┌─────────────────────────────────────────────────────────┐
│                  internal/adapter/                       │
│  (Primary/Driving Adapters)                              │
│  http/handler/   — HTTP → calls Service port            │
│  http/middleware/ — trace ID, logging                   │
│  http/v1/router/ — route registration                   │
└───────────────────────┬─────────────────────────────────┘
                        │ depends on
┌───────────────────────▼─────────────────────────────────┐
│                    internal/core/                        │
│  (Ports — interfaces that define the boundary)           │
│  */port.go  — Service interface (driven by adapter)      │
│             — Repository interface (drives infra)        │
│  */service.go — business logic, depends only on ports   │
└───────────────────────┬─────────────────────────────────┘
                        │ depends on
┌───────────────────────▼─────────────────────────────────┐
│                    internal/infra/                       │
│  (Secondary/Driven Adapters)                             │
│  database/repository/domain/ — implements Repository    │
│  database/repository/cqrs/  — CQRS query/command base   │
│  database/dao/              — DB row structs only        │
│  configloader/              — env config                 │
│  logger/                    — structured logging         │
└─────────────────────────────────────────────────────────┘
```

### Layer Rules

| Layer | Package | Rule |
|-------|---------|------|
| Primary adapter | `internal/adapter/` | Calls Service port only. No business logic, no direct DB access. |
| Port | `internal/core/*/port.go` | Interface definitions only. Owned by core, not by any adapter. |
| Core | `internal/core/*/service.go` | Business logic. Depends on Repository port — never on infra directly. |
| Secondary adapter | `internal/infra/database/` | Implements Repository port. No business logic. |
| Shared structs | `internal/infra/database/dao/` | DB row structs only. Imported by infra layer. |

## Domain Model

### AccountBalances
Represents a bank/ledger account with a current balance.
- Fields: `account_number`, `holder_name`, `balance`, `currency`, `status`, `frozen_at`, `closed_at`
- Status values: active / frozen / closed

### LedgerEntries
Double-entry bookkeeping records — one debit and one credit per transaction.
- Fields: `transaction_id`, `account_id`, `entry_type` (debit/credit), `amount`, `currency`, `balance_before`, `balance_after`, `description`

### TransactionTransfers
Represents a fund transfer between two accounts.
- Fields: `reference_id`, `source_account_id`, `destination_account_id`, `amount`, `currency`, `status`, `completed_at`, `failed_at`

## HTTP Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/health` | Health check (DB ping) |
| `POST` | `/api/v1/health` | Health check (DB ping) |
| `POST` | `/api/v1/health/echo` | Echo request payload |
| `POST` | `/api/v1/transfer` | Create fund transfer |

## Module Name

`github.com/dhuki/go-ledger-system`

## Common Import Aliases

```go
"github.com/dhuki/go-ledger-system/internal/infra/configloader"
"github.com/dhuki/go-ledger-system/internal/infra/logger"
"github.com/dhuki/go-ledger-system/internal/infra/database"
"github.com/dhuki/go-ledger-system/internal/infra/database/dao"
"github.com/dhuki/go-ledger-system/internal/core/transaction"
"github.com/dhuki/go-ledger-system/internal/core/healthcheck"
"github.com/dhuki/go-ledger-system/internal/adapter/http/middleware"
```
