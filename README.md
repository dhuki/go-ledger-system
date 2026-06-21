# Ledger System Apps

A backend service that manages bank ledger records using double-entry bookkeeping. Every transaction posts matching debit and credit entries atomically, ensuring balances always remain consistent.

---

## 1. Running with Docker

### Prepare configuration

Copy the example file and fill in your values:

```bash
cp .env.example .env
```

Required values in `.env`:

```env
# Application
APP_NAME=go-ledger-system
REST_PORT=8080
GRACEFUL_TIMEOUT=30s

# PostgreSQL
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_DBNAME=ledger_system
POSTGRES_USERNAME=postgres
POSTGRES_PASSWORD=your_password
POSTGRES_SSL_MODE=disable
POSTGRES_MIGRATION_DIR=internal/infra/database/repository/domain/migration
POSTGRES_MAX_CONNECTION=10
POSTGRES_MAX_IDLE_CONNECTION=5
POSTGRES_MAX_DURATION_IDLE_CONN=5m
POSTGRES_MAX_DURATION_LIFETIME_CONN=1h

# Redis
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0
REDIS_TRANSFER_IDEMPOTENCY_TTL=1h
```

> 💡 **Tip — `POSTGRES_HOST` and `REDIS_HOST`:** These values in `.env` are only used when running the app locally. Docker Compose overrides them to `postgres` and `redis` so the app container resolves correctly within the Docker network.

> ⚠️ **Warning — `POSTGRES_MIGRATION_DIR` must not be changed.** The Dockerfile bakes the migration files into the container at the fixed path `internal/infra/database/repository/domain/migration`. If you change this value the app will fail to find the migration files on startup and will exit immediately.

> ⚠️ **Warning — changing PostgreSQL credentials on an existing volume will crash the app.** PostgreSQL only reads `POSTGRES_DBNAME`, `POSTGRES_USERNAME`, and `POSTGRES_PASSWORD` when initializing a brand-new data directory. If the `postgres_data` Docker volume already exists from a previous `make up`, restarting with different credentials leaves the old ones intact in the volume while the app tries to connect with the new ones — causing an authentication failure and a failed startup. To apply new credentials safely, remove the volume first:
>
> ```bash
> make down
> docker volume rm go-ledger-system_postgres_data
> make up
> ```

### Start all services

```bash
make up
```

This builds the app image and starts three containers:


| Container                  | Role          | Exposed port    |
| -------------------------- | ------------- | --------------- |
| `go-ledger-system-psql`    | PostgreSQL 16 | `5432`, `15432` |
| `go-ledger-system-redis`   | Redis 7       | `6379`          |
| `go-ledger-system-service` | Application   | `8080`          |

The app waits for PostgreSQL and Redis health checks to pass before starting. Database migrations run automatically on startup.

### Lifecycle commands

```bash
make up         # build and start all services
make down       # stop and remove all containers
make restart    # down + rebuild + up
```

---

### Calling the endpoints

Base URL: `http://localhost:8080`

#### Health check

```bash
curl -s http://localhost:8080/api/v1/health | jq
```

Expected response:

```json
{
  "status": 200,
  "message": "ok",
  "data": null
}
```

#### Create a fund transfer

```bash
curl -s -X POST http://localhost:8080/api/v1/transfer \
  -H "Content-Type: application/json" \
  -H "X-Idempotency-Key: unique-key-001" \
  -H "X-Trace-ID: $(uuidgen)" \
  -d '{
    "sender_account": "ACC-001",
    "receiver_account": "ACC-002",
    "amount": 500000
  }' | jq
```

**Request headers**


| Header              | Required | Description                                                              |
| ------------------- | -------- | ------------------------------------------------------------------------ |
| `Content-Type`      | Yes      | Must be`application/json`                                                |
| `X-Idempotency-Key` | Yes      | Unique key per transfer intent; prevents duplicate processing on retries |
| `X-Trace-ID`        | Optional | UUID for request tracing across logs                                     |

**Request body**


| Field              | Type    | Description                                                                |
| ------------------ | ------- | -------------------------------------------------------------------------- |
| `sender_account`   | string  | Account number to debit                                                    |
| `receiver_account` | string  | Account number to credit                                                   |
| `amount`           | integer | Transfer amount in the smallest currency unit — must be greater than zero |

**Success response** (`200 OK`):

```json
{
  "status": 200,
  "message": "ok",
  "data": {
    "transaction_id": 1
  }
}
```

**Error responses**


| HTTP status                | Cause                                                      |
| -------------------------- | ---------------------------------------------------------- |
| `400 Bad Request`          | `amount` ≤ 0, or sender and receiver are the same account |
| `404 Not Found`            | Sender or receiver account does not exist                  |
| `422 Unprocessable Entity` | Sender has insufficient balance---                         |


## 2. Running Integration Tests

Integration tests run against live Docker containers and verify end-to-end behaviour including idempotency, concurrency consistency, and deadlock prevention.

### Prerequisites

- All Docker containers must be running (`make up`).
- The test uses port `15432` (the extra PostgreSQL port exposed by Docker Compose) to connect directly to the database for assertions and state resets.

> **Warning:** The credentials in `DOCKER_PSQL_DSN` (`user`, `password`, `dbname`) must exactly match the values you set in `.env` for `POSTGRES_USERNAME`, `POSTGRES_PASSWORD`, and `POSTGRES_DBNAME`. A mismatch will cause the test to fail to connect to the database inside the Docker container. Double-check your `.env` before running.

### Run

```bash
make test-integration
```

The Makefile sets `DOCKER_PSQL_DSN` automatically. To run manually with a custom DSN:

```bash
DOCKER_PSQL_DSN="host=localhost port=15432 dbname=<dbname> user=<user> password=<password> sslmode=disable" \
  go test -v -count=1 -timeout=60s ./scripts/integration/...
```

### What the tests cover


| Test                                         | Description                                                                                      |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| `TestIntegration_Validation`                 | Rejects zero amount, self-transfer, unknown sender, and insufficient balance                     |
| `TestIntegration_IdempotencyConcurrency`     | 50 concurrent requests sharing one idempotency key — exactly one transfer must be persisted     |
| `TestIntegration_HighConcurrencyConsistency` | 300 concurrent unique transfers across 3 accounts — total money in the system must be conserved |
| `TestIntegration_NoDeadlock_Transfers`       | 150 reciprocal A→B / B→A pairs fired simultaneously — no deadlocks, no 5xx responses          |

> **Warning:** Each test truncates `ledger_entries` and `transaction_transfers` and resets account balances before running. Do not point `DOCKER_PSQL_DSN` at a database with data you want to keep.

---

## 3. Integration Test Results

Run with `make test-integration`. All four suites pass. Screenshots are provided alongside the captured output for each suite.

---

### Validation test

Verifies that malformed or invalid requests are rejected before touching the database — zero amount, self-transfer, unknown sender, unknown receiver, and insufficient balance.

![Validation test result](docs/test_result_validation.png)

<details>
<summary>Test output</summary>

```
=== RUN   TestIntegration_Validation

=== TestIntegration_Validation ===
--------------------------------------------------------------------
CASE: zero amount transfer

  INPUT:
    sender               = ACC-001
    receiver             = ACC-002
    amount               = 0

  RESPONSE:
    response.status        got: 400                             expected: 400
    response.body          got: amount must be greater than zero  expected: amount must be greater than zero

  BALANCE STATE:
    account       seed                actual              delta
    ACC-001       10000000            10000000            +0
    ACC-002       5000000             5000000             +0

  VALIDATION:
    case                                got                expected           result
    response.status                     400                400                PASS
    response.message                    amount must be greater than zero amount must be greater than zero PASS
    database.count_created_transaction  0                  0                  PASS
    database.count_created_ledger       0                  0                  PASS
    database.balance_ACC-001            10000000           10000000           PASS
    database.balance_ACC-002            5000000            5000000            PASS

  OVERALL: PASS
--------------------------------------------------------------------
--------------------------------------------------------------------
CASE: self transfer

  INPUT:
    sender               = ACC-001
    receiver             = ACC-001
    amount               = 100

  RESPONSE:
    response.status        got: 400                             expected: 400
    response.body          got: cannot transfer to the same account  expected: cannot transfer to the same account

  BALANCE STATE:
    account       seed                actual              delta
    ACC-001       10000000            10000000            +0

  VALIDATION:
    case                                got                expected           result
    response.status                     400                400                PASS
    response.message                    cannot transfer to the same account cannot transfer to the same account PASS
    database.count_created_transaction  0                  0                  PASS
    database.count_created_ledger       0                  0                  PASS
    database.balance_ACC-001            10000000           10000000           PASS

  OVERALL: PASS
--------------------------------------------------------------------
--------------------------------------------------------------------
CASE: sender not found

  INPUT:
    sender               = ACC-999
    receiver             = ACC-002
    amount               = 100

  RESPONSE:
    response.status        got: 404                             expected: 404
    response.body          got: account not found               expected: account not found

  BALANCE STATE:
    account       seed                actual              delta
    ACC-999       0                   not found           -
    ACC-002       5000000             5000000             +0

  VALIDATION:
    case                                got                expected           result
    response.status                     404                404                PASS
    response.message                    account not found  account not found  PASS
    database.count_created_transaction  0                  0                  PASS
    database.count_created_ledger       0                  0                  PASS
    database.balance_ACC-002            5000000            5000000            PASS

  OVERALL: PASS
--------------------------------------------------------------------
--------------------------------------------------------------------
CASE: receiver not found

  INPUT:
    sender               = ACC-002
    receiver             = ACC-999
    amount               = 100

  RESPONSE:
    response.status        got: 404                             expected: 404
    response.body          got: account not found               expected: account not found

  BALANCE STATE:
    account       seed                actual              delta
    ACC-002       5000000             5000000             +0
    ACC-999       0                   not found           -

  VALIDATION:
    case                                got                expected           result
    response.status                     404                404                PASS
    response.message                    account not found  account not found  PASS
    database.count_created_transaction  0                  0                  PASS
    database.count_created_ledger       0                  0                  PASS
    database.balance_ACC-002            5000000            5000000            PASS

  OVERALL: PASS
--------------------------------------------------------------------
--------------------------------------------------------------------
CASE: insufficient balance

  INPUT:
    sender               = ACC-003
    receiver             = ACC-001
    amount               = 1

  RESPONSE:
    response.status        got: 422                             expected: 422
    response.body          got: insufficient balance            expected: insufficient balance

  BALANCE STATE:
    account       seed                actual              delta
    ACC-003       0                   0                   +0
    ACC-001       10000000            10000000            +0

  VALIDATION:
    case                                got                expected           result
    response.status                     422                422                PASS
    response.message                    insufficient balance insufficient balance PASS
    database.count_created_transaction  0                  0                  PASS
    database.count_created_ledger       0                  0                  PASS
    database.balance_ACC-003            0                  0                  PASS
    database.balance_ACC-001            10000000           10000000           PASS

  OVERALL: PASS
--------------------------------------------------------------------
--- PASS: TestIntegration_Validation (0.36s)
```

</details>

---

### Idempotency & concurrency test

50 goroutines fire the same transfer simultaneously using one idempotency key. Exactly one debit must be persisted regardless of how many requests reach the server.

![Idempotency concurrency test result](docs/test_result_idempotency.png)

<details>
<summary>Test output</summary>

```
=== RUN   TestIntegration_IdempotencyConcurrency

=== TestIntegration_IdempotencyConcurrency ===
--------------------------------------------------------------------
CASE: 50 concurrent requests — same idempotency key

  INPUT:
    workers              = 50
    sender               = ACC-001
    receiver             = ACC-002
    amount               = 1000000
    idempotency_key      = idem-1782026078359813000

  BALANCE STATE:
    account       seed                actual              delta
    ACC-001       10000000            9000000             -1000000
    ACC-002       5000000             6000000             +1000000

  VALIDATION:
    case                                got                expected           result
    response.status_5xx                 0                  0                  PASS
    database.count_success_transfer     1                  1                  PASS
    database.balance_ACC-001            9000000            9000000            PASS
    database.balance_ACC-002            6000000            6000000            PASS
    database.count_created_ledger       2                  2                  PASS

  OVERALL: PASS
--------------------------------------------------------------------
--- PASS: TestIntegration_IdempotencyConcurrency (0.10s)
```

</details>

---

### High-concurrency consistency test

500 goroutines perform unique transfers across 3 accounts in a circular pattern (ACC-001→ACC-002→ACC-003→ACC-001). Total money in the system must be conserved and the ledger must account for every credit and debit.

The `credit_in` and `debit_out` values under each account show the gross transfer volume. Because the pattern is circular each account sends and receives a similar total, keeping the net delta close to zero — this is by design and proves conservation, not stagnation.

![High concurrency consistency test result](docs/test_result_consistency.png)

<details>
<summary>Test output</summary>

```
=== RUN   TestIntegration_HighConcurrencyConsistency

=== TestIntegration_HighConcurrencyConsistency ===
--------------------------------------------------------------------
CASE: 500 concurrent unique transfers across 3 accounts

  INPUT:
    workers              = 500
    amount_each          = 3000
    accounts             = ACC-001, ACC-002, ACC-003
    total_money_in_system = 15500000

  BALANCE STATE:
    account       seed                actual              delta
    ACC-001       10000000            9997000             -3000
                  credit_in: 498000   debit_out: 501000

    ACC-002       5000000             5000000             +0
                  credit_in: 501000   debit_out: 501000

    ACC-003       500000              503000              +3000
                  credit_in: 501000   debit_out: 498000

  VALIDATION:
    case                                got                expected           result
    response.status_5xx                 0                  0                  PASS
    total_money_conserved               15500000           15500000           PASS
    system.credit_in_vs_moving_amount   1500000            1500000            PASS
    system.debit_out_vs_moving_amount   1500000            1500000            PASS
    database.balance_ACC-001_ledger     9997000            9997000            PASS
    database.balance_ACC-002_ledger     5000000            5000000            PASS
    database.balance_ACC-003_ledger     503000             503000             PASS

  OVERALL: PASS
--------------------------------------------------------------------
--- PASS: TestIntegration_HighConcurrencyConsistency (1.61s)
```

</details>

---

### Deadlock prevention test

300 goroutines fire 150 reciprocal A→B / B→A pairs simultaneously. Consistent lock-acquisition order must prevent all deadlocks. Total money across both accounts must be conserved and the ledger must have exactly 2 entries per transfer.

![Deadlock prevention test result](docs/test_result_deadlock.png)

<details>
<summary>Test output</summary>

```
=== RUN   TestIntegration_NoDeadlock_Transfers

=== TestIntegration_NoDeadlock_Transfers ===
--------------------------------------------------------------------
CASE: 150 reciprocal pairs ACC-001<->ACC-002 (total 300 goroutines)

  INPUT:
    pairs                = 150
    amount_each          = 100
    ACC-001.balance_before = 100000000
    ACC-002.balance_before = 100000000

  BALANCE STATE:
    account       seed                actual              delta
    ACC-001       100000000           100000000           +0
    ACC-002       100000000           100000000           +0

  VALIDATION:
    case                                got                expected           result
    5xx_responses                       0                  0                  PASS
    total_money_conserved               200000000          200000000          PASS
    ledger_entries_per_transfer (ratio*transfers) 600       600                PASS

  OVERALL: PASS
--------------------------------------------------------------------
--- PASS: TestIntegration_NoDeadlock_Transfers (1.03s)
PASS
ok      github.com/dhuki/go-ledger-system/scripts/integration   3.685s
```

</details>
