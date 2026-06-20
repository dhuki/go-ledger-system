---
name: mockery-skill
description: >
  Generates Go mock implementations using mockery (github.com/vektra/mockery).
  Use this skill whenever the user asks to generate mocks, create a mock for an
  interface, or update existing generated mocks — even if they don't say "mockery"
  explicitly. Trigger on phrases like "generate mock", "create mock for", "mock
  interface", "run mockery", "regenerate mocks", or "add mock".
---

# Mockery Skill

This project uses [mockery v2](https://github.com/vektra/mockery) to generate
Go mock implementations from interfaces.

Module: `github.com/dhuki/go-ledger-system`

---

## Step 0 — Ensure mockery is installed

```bash
which mockery || go install github.com/vektra/mockery/v2@latest
```

If installation fails, ask the user to run it manually.

---

## Step 1 — Locate the interface

All mockable interfaces in this project live in these files:

| Package path | File | Interfaces |
|---|---|---|
| `internal/core/transaction` | `port.go` | `Service`, `Repository`, `Cache` |
| `internal/core/healthcheck` | `port.go` | `Service`, `Repository`, `Cache` |
| `internal/infra/database/repository/domain` | `base_domain.go` | `DomainRepository` |
| `internal/infra/database/repository/domain` | `account_balance.go` | `TrxAccountBalance` |
| `internal/infra/database/repository/domain` | `transaction_transfer.go` | `TrxTransfer` |
| `internal/infra/database/repository/domain` | `ledger_entry.go` | `TrxLedgerEntry` |
| `internal/infra/database/repository/domain` | `transaction.go` | `Transaction` |
| `internal/infra/database/repository/domain` | `health_check.go` | `HealthCheck` |
| `internal/infra/cache` | `redis.go` | `RedisClient` |

---

## Step 2 — Decide where mocks go

Generated mocks live in a `mocks/` subdirectory **inside the same package directory**
as the interface.

| Interface source | Mock output directory |
|---|---|
| `internal/core/transaction/port.go` | `internal/core/transaction/mocks/` |
| `internal/core/healthcheck/port.go` | `internal/core/healthcheck/mocks/` |
| `internal/infra/database/repository/domain/*.go` | `internal/infra/database/repository/domain/mocks/` |
| `internal/infra/cache/redis.go` | `internal/infra/cache/mocks/` |

The generated file is named `Mock{InterfaceName}.go` (e.g. `MockService.go`).
The generated package name is `mocks`.

---

## Step 3 — Generate with the CLI

Run mockery from the **project root**. Always pass `--with-expecter` for typed
`EXPECT()` helpers and `--keeptree` to preserve the output directory layout.

### Single interface

```bash
mockery \
  --name=<InterfaceName> \
  --dir=<path/to/package> \
  --output=<path/to/package>/mocks \
  --outpkg=mocks \
  --with-expecter
```

**Examples:**

```bash
# Mock the transaction Service port
mockery \
  --name=Service \
  --dir=internal/core/transaction \
  --output=internal/core/transaction/mocks \
  --outpkg=mocks \
  --with-expecter

# Mock the transaction Repository port
mockery \
  --name=Repository \
  --dir=internal/core/transaction \
  --output=internal/core/transaction/mocks \
  --outpkg=mocks \
  --with-expecter

# Mock DomainRepository (infra layer)
mockery \
  --name=DomainRepository \
  --dir=internal/infra/database/repository/domain \
  --output=internal/infra/database/repository/domain/mocks \
  --outpkg=mocks \
  --with-expecter

# Mock the cache interface in the transaction core port
mockery \
  --name=Cache \
  --dir=internal/core/transaction \
  --output=internal/core/transaction/mocks \
  --outpkg=mocks \
  --with-expecter
```

### All interfaces at once using `.mockery.yaml`

For bulk regeneration, create or update `.mockery.yaml` at the project root,
then run `mockery` with no arguments.

**`.mockery.yaml` template for this project:**

```yaml
with-expecter: true
quiet: true
packages:
  github.com/dhuki/go-ledger-system/internal/core/transaction:
    config:
      dir: internal/core/transaction/mocks
      outpkg: mocks
    interfaces:
      Service:
      Repository:
      Cache:

  github.com/dhuki/go-ledger-system/internal/core/healthcheck:
    config:
      dir: internal/core/healthcheck/mocks
      outpkg: mocks
    interfaces:
      Service:
      Repository:
      Cache:

  github.com/dhuki/go-ledger-system/internal/infra/database/repository/domain:
    config:
      dir: internal/infra/database/repository/domain/mocks
      outpkg: mocks
    interfaces:
      DomainRepository:
      TrxAccountBalance:
      TrxTransfer:
      TrxLedgerEntry:
      Transaction:
      HealthCheck:

  github.com/dhuki/go-ledger-system/internal/infra/cache:
    config:
      dir: internal/infra/cache/mocks
      outpkg: mocks
    interfaces:
      RedisClient:
```

Then regenerate everything:

```bash
mockery
```

---

## Step 4 — Verify

After generating, confirm the output compiles:

```bash
go build ./...
```

If a generated mock fails to compile because an interface method changed,
regenerate it with the same command from Step 3.

---

## Using generated mocks in tests

Generated mocks live in the `mocks` sub-package. Import them with an alias to
avoid name collision with the real package.

```go
import (
    txmocks "github.com/dhuki/go-ledger-system/internal/core/transaction/mocks"
)

func TestSomething(t *testing.T) {
    mockRepo := txmocks.NewMockRepository(t)

    // Expect a specific call with typed args and set return values.
    mockRepo.EXPECT().
        GetTransferByReferenceID(mock.Anything, "key-123").
        Return(nil, nil).
        Once()

    svc := transaction.NewService(mockRepo, mockCache)
    // ... test body
}
```

**Rules for tests using mockery mocks:**
- Always pass `t` to `NewMock{Name}(t)` — this registers automatic assertion of expectations at test end.
- Use `mock.Anything` for context arguments (`ctx`).
- Prefer `.Once()` / `.Times(n)` over open-ended expectations to catch unexpected extra calls.
- Do not mix mockery mocks with hand-written mocks in the same test file.

---

## When to use mockery vs hand-written mocks

| Situation | Use |
|---|---|
| Unit test for a single service method, simple flow | hand-written mock (faster to write) |
| Need to assert exact call counts or argument values | mockery (`EXPECT()`) |
| Multiple tests need the same mock with different behaviours | mockery (avoid duplicating mock logic) |
| Integration test with real DB/Redis | no mock — use real implementations |
| Concurrency / race tests where mock must simulate real locking | hand-written mock (full control over mutexes) |

---

## Checklist before finishing

1. Did `mockery` run without errors?
2. Does `go build ./...` pass?
3. Is the generated file in the correct `mocks/` subdirectory?
4. Does the mock package name match `mocks` (not the parent package name)?
5. If `.mockery.yaml` was created or updated — does it include every interface the user asked for?
