//go:build integration

// Package integration exercises the full transfer stack — HTTP handler →
// middleware → service → repository → PostgreSQL — against a real database.
//
// It is gated behind the `integration` build tag and skips unless LEDGER_TEST_DSN
// points at a reachable PostgreSQL instance, so it never runs during plain
// `go test ./...`.
//
// Run it against the docker-compose database with:
//
//	LEDGER_TEST_DSN="host=localhost port=5432 dbname=ledger user=ledger password=ledger sslmode=disable" \
//	    go test -tags=integration -race -count=1 ./test/integration/...
package test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"

	"github.com/dhuki/go-ledger-system/internal/adapter/http/handler"
	"github.com/dhuki/go-ledger-system/internal/adapter/http/middleware"
	"github.com/dhuki/go-ledger-system/internal/adapter/http/model"
	"github.com/dhuki/go-ledger-system/internal/core/transaction"
	"github.com/dhuki/go-ledger-system/internal/infra/database/repository"
	"github.com/dhuki/go-ledger-system/internal/infra/database/repository/domain"
)

const (
	migrationDir = "../../internal/infra/database/repository/domain/migration"
	transferPath = "/api/v1/transfer"
)

// seed balances reset before every test for deterministic assertions.
var seed = map[string]int64{
	"ACC-001": 10_000_000,
	"ACC-002": 5_000_000,
	"ACC-003": 0,
}

var (
	harnessOnce  sync.Once
	sharedDB     *sql.DB
	sharedServer *httptest.Server
	harnessErr   error
)

// harness lazily opens the database, runs migrations once, and stands up the
// real HTTP server. It skips the test when no DSN is configured.
func harness(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()

	dsn := os.Getenv("LEDGER_TEST_DSN")
	if dsn == "" {
		t.Skip("LEDGER_TEST_DSN not set; skipping integration test")
	}

	harnessOnce.Do(func() {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			harnessErr = fmt.Errorf("open db: %w", err)
			return
		}
		// Allow real parallelism so the concurrency tests exercise actual row locks.
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(25)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			harnessErr = fmt.Errorf("ping db: %w", err)
			return
		}
		if err := runMigrations(db); err != nil {
			harnessErr = fmt.Errorf("migrate: %w", err)
			return
		}

		sharedDB = db
		sharedServer = httptest.NewServer(newRouter(db))
	})

	if harnessErr != nil {
		t.Fatalf("integration harness setup failed: %v", harnessErr)
	}
	return sharedServer, sharedDB
}

func runMigrations(db *sql.DB) error {
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(db, migrationDir)
}

// newRouter wires the production components exactly as cmd/server does.
func newRouter(db *sql.DB) http.Handler {
	gin.SetMode(gin.TestMode)

	domainRepo := domain.NewRepositoryDomain(repository.NewQueryRepository(db))
	svc := transaction.NewService(domainRepo)
	transferHandler := handler.NewTransactionHandler(svc)

	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(middleware.CollectMetadataHeader())

	v1 := engine.Group("/api/v1")
	transferHandler.RegisterRoute(v1)
	return engine
}

// resetState returns the database to the known seed before each test.
func resetState(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`TRUNCATE ledger_entries, transaction_transfers, account_balances RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	for number, balance := range seed {
		if _, err := db.Exec(
			`INSERT INTO account_balances (account_number, holder_name, balance, currency, status)
			 VALUES ($1, $2, $3, 'IDR', 'active')`,
			number, number, balance,
		); err != nil {
			t.Fatalf("seed %s: %v", number, err)
		}
	}
}

// postTransfer sends one transfer request and returns the HTTP status code.
func postTransfer(t *testing.T, serverURL, idempotencyKey, sender, receiver string, amount int64) int {
	t.Helper()

	body, err := json.Marshal(model.CreateTransferRequest{
		SenderAccount:   sender,
		ReceiverAccount: receiver,
		Amount:          amount,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, serverURL+transferPath, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trace-ID", "it-"+idempotencyKey)
	if idempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", idempotencyKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// --- DB assertion helpers ---

func balanceOf(t *testing.T, db *sql.DB, number string) int64 {
	t.Helper()
	var balance int64
	if err := db.QueryRow(`SELECT balance FROM account_balances WHERE account_number = $1`, number).Scan(&balance); err != nil {
		t.Fatalf("balanceOf %s: %v", number, err)
	}
	return balance
}

func scalarInt(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return n
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

func TestIntegration_Validation(t *testing.T) {
	srv, db := harness(t)
	resetState(t, db)

	if status := postTransfer(t, srv.URL, "key-zero-amount", "ACC-001", "ACC-002", 0); status != http.StatusBadRequest {
		t.Errorf("zero amount status = %d, want 400", status)
	}
	if status := postTransfer(t, srv.URL, "key-self", "ACC-001", "ACC-001", 100); status != http.StatusBadRequest {
		t.Errorf("self transfer status = %d, want 400", status)
	}
	// Nothing should have moved.
	if got := scalarInt(t, db, `SELECT COUNT(*) FROM transaction_transfers`); got != 0 {
		t.Errorf("transfer rows = %d, want 0", got)
	}
}

// TestIntegration_IdempotencyUnderConcurrency fires many concurrent requests
// that share one idempotency key against the real database. Exactly one transfer
// must be persisted and the sender debited exactly once.
func TestIntegration_IdempotencyUnderConcurrency(t *testing.T) {
	srv, db := harness(t)
	resetState(t, db)

	const (
		workers = 50
		amount  = 1_000_000
	)
	key := "idem-" + fmt.Sprint(time.Now().UnixNano())

	var wg sync.WaitGroup
	start := make(chan struct{})
	var serverErrors int64

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			status := postTransfer(t, srv.URL, key, "ACC-001", "ACC-002", amount)
			if status >= 500 {
				atomic.AddInt64(&serverErrors, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if serverErrors != 0 {
		t.Errorf("got %d 5xx responses, want 0", serverErrors)
	}
	if got := scalarInt(t, db, `SELECT COUNT(*) FROM transaction_transfers WHERE reference_id = $1`, key); got != 1 {
		t.Errorf("transfers for key = %d, want 1 (idempotency violated)", got)
	}
	if got := balanceOf(t, db, "ACC-001"); got != seed["ACC-001"]-amount {
		t.Errorf("sender balance = %d, want %d (debited exactly once)", got, seed["ACC-001"]-amount)
	}
	if got := balanceOf(t, db, "ACC-002"); got != seed["ACC-002"]+amount {
		t.Errorf("receiver balance = %d, want %d", got, seed["ACC-002"]+amount)
	}
	if got := scalarInt(t, db, `SELECT COUNT(*) FROM ledger_entries`); got != 2 {
		t.Errorf("ledger entries = %d, want 2", got)
	}
}

// TestIntegration_HighConcurrencyConsistency drives many concurrent unique
// transfers through the real stack and verifies the ledger stays perfectly
// consistent: money conserved, no negative balances, two ledger rows per
// transfer, and every balance reconciles with its journal.
func TestIntegration_HighConcurrencyConsistency(t *testing.T) {
	srv, db := harness(t)
	resetState(t, db)

	accounts := []string{"ACC-001", "ACC-002", "ACC-003"}
	const (
		workers = 300
		amount  = 1_000
	)

	var wg sync.WaitGroup
	start := make(chan struct{})
	var serverErrors int64

	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sender := accounts[i%len(accounts)]
			receiver := accounts[(i+1)%len(accounts)]
			key := fmt.Sprintf("consistency-%d-%d", time.Now().UnixNano(), i)
			<-start
			if status := postTransfer(t, srv.URL, key, sender, receiver, amount); status >= 500 {
				atomic.AddInt64(&serverErrors, 1)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if serverErrors != 0 {
		t.Errorf("got %d 5xx responses, want 0", serverErrors)
	}

	var total int64
	for _, acc := range accounts {
		bal := balanceOf(t, db, acc)
		if bal < 0 {
			t.Errorf("account %s went negative: %d", acc, bal)
		}
		total += bal
	}
	if total != 15_000_000 {
		t.Errorf("total balance = %d, want 15000000 (money created/destroyed)", total)
	}

	transfers := scalarInt(t, db, `SELECT COUNT(*) FROM transaction_transfers`)
	ledgers := scalarInt(t, db, `SELECT COUNT(*) FROM ledger_entries`)
	if ledgers != transfers*2 {
		t.Errorf("ledger entries = %d, want %d (2 per transfer)", ledgers, transfers*2)
	}

	// Each balance must equal its seed plus the net of its ledger entries.
	for _, acc := range accounts {
		delta := scalarInt(t, db, `
			SELECT COALESCE(SUM(CASE WHEN le.entry_type = 'credit' THEN le.amount ELSE -le.amount END), 0)
			FROM ledger_entries le
			JOIN account_balances ab ON ab.id = le.account_id
			WHERE ab.account_number = $1`, acc)
		if got, want := balanceOf(t, db, acc), seed[acc]+delta; got != want {
			t.Errorf("account %s balance = %d, but ledger implies %d", acc, got, want)
		}
	}
}

// TestIntegration_NoDeadlock_ReciprocalTransfers hammers the real database with
// reciprocal A->B / B->A transfers. PostgreSQL would abort a deadlocked
// transaction (surfacing as a 5xx); consistent lock ordering keeps all requests
// succeeding and money conserved.
func TestIntegration_NoDeadlock_ReciprocalTransfers(t *testing.T) {
	srv, db := harness(t)
	resetState(t, db)
	// Top up so neither side runs out and we always exercise the lock path.
	if _, err := db.Exec(`UPDATE account_balances SET balance = 100000000 WHERE account_number IN ('ACC-001','ACC-002')`); err != nil {
		t.Fatalf("top up: %v", err)
	}

	const pairs = 150
	var wg sync.WaitGroup
	start := make(chan struct{})
	var serverErrors int64

	fire := func(sender, receiver string, i int) {
		defer wg.Done()
		key := fmt.Sprintf("deadlock-%s-%d-%d", sender, time.Now().UnixNano(), i)
		<-start
		if status := postTransfer(t, srv.URL, key, sender, receiver, 100); status >= 500 {
			atomic.AddInt64(&serverErrors, 1)
		}
	}

	for i := range pairs {
		wg.Add(2)
		go fire("ACC-001", "ACC-002", i)
		go fire("ACC-002", "ACC-001", i)
	}

	done := make(chan struct{})
	go func() {
		close(start)
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out: reciprocal transfers likely deadlocked")
	}

	if serverErrors != 0 {
		t.Errorf("got %d 5xx responses, want 0 (deadlock or rollback failure)", serverErrors)
	}
	total := balanceOf(t, db, "ACC-001") + balanceOf(t, db, "ACC-002")
	if total != 200_000_000 {
		t.Errorf("total balance = %d, want 200000000", total)
	}
}
