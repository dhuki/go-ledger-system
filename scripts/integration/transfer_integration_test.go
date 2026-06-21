package integration

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

// TestIntegration_Validation verifies that malformed requests are rejected
// before touching the database.
func TestIntegration_Validation(t *testing.T) {
	checkServer(t)
	db := openDB(t)
	resetState(t, db)

	cases := []struct {
		name          string
		sender        string
		receiver      string
		amount        int64
		wantStatus    int
		wantMessage   string // expected BaseResponse.Message
		wantTransfers int64
		wantSenderBal int64
		wantRecBal    int64
	}{
		{
			name:   "zero amount transfer",
			sender: "ACC-001", receiver: "ACC-002", amount: 0,
			wantStatus:    http.StatusBadRequest,
			wantMessage:   "amount must be greater than zero",
			wantTransfers: 0,
			wantSenderBal: seed["ACC-001"], wantRecBal: seed["ACC-002"],
		},
		{
			name:   "self transfer",
			sender: "ACC-001", receiver: "ACC-001", amount: 100,
			wantStatus:    http.StatusBadRequest,
			wantMessage:   "cannot transfer to the same account",
			wantTransfers: 0,
			wantSenderBal: seed["ACC-001"], wantRecBal: seed["ACC-001"],
		},
		{
			name:   "sender not found",
			sender: "ACC-999", receiver: "ACC-002", amount: 100,
			wantStatus:    http.StatusNotFound,
			wantMessage:   "account not found",
			wantTransfers: 0,
			wantSenderBal: 0,
			wantRecBal:    seed["ACC-002"],
		},
		{
			name:   "receiver not found",
			sender: "ACC-002", receiver: "ACC-999", amount: 100,
			wantStatus:    http.StatusNotFound,
			wantMessage:   "account not found",
			wantTransfers: 0,
			wantSenderBal: seed["ACC-002"],
			wantRecBal:    0,
		},
		{
			name:   "insufficient balance",
			sender: "ACC-003", receiver: "ACC-001", amount: 1,
			wantStatus:    http.StatusUnprocessableEntity,
			wantMessage:   "insufficient balance",
			wantTransfers: 0,
			wantSenderBal: seed["ACC-003"], wantRecBal: seed["ACC-001"],
		},
	}

	fmt.Printf("\n=== TestIntegration_Validation ===\n")
	for _, tc := range cases {
		resetState(t, db)
		key := uuid.NewString()

		r := newReport(t, tc.name)
		r.input("sender", tc.sender)
		r.input("receiver", tc.receiver)
		r.input("amount", fmt.Sprintf("%d", tc.amount))
		r.input("idempotency_key", key)
		r.expectResponse(tc.wantMessage)

		status, body := postTransfer(t, key, tc.sender, tc.receiver, tc.amount)
		r.setResponse(status, body)

		r.checkHTTP(status, tc.wantStatus)
		r.checkMessage(body.Message, tc.wantMessage)
		r.checkInt("database.count_created_transaction",
			scalarInt(t, db, `SELECT COUNT(*) FROM transaction_transfers`),
			tc.wantTransfers)
		r.checkInt("database.count_created_ledger",
			scalarInt(t, db, `SELECT COUNT(*) FROM ledger_entries`),
			0)

		label := "database.balance_%s"
		if bal, ok := queryBalance(db, tc.sender); ok {
			r.checkInt(fmt.Sprintf(label, tc.sender), bal, tc.wantSenderBal)
		}
		if bal, ok := queryBalance(db, tc.receiver); ok {
			r.checkInt(fmt.Sprintf(label, tc.receiver), bal, tc.wantRecBal)
		}

		seen := map[string]bool{}
		for _, acc := range []string{tc.sender, tc.receiver} {
			if acc == "" || seen[acc] {
				continue
			}
			seen[acc] = true
			r.recordBalanceWithFlow(acc, seed[acc], db)
		}

		r.print()
	}
}

// TestIntegration_IdempotencyConcurrency fires many concurrent requests that
// share one idempotency key. Exactly one transfer must be persisted and the
// sender debited exactly once.
func TestIntegration_IdempotencyConcurrency(t *testing.T) {
	checkServer(t)
	db := openDB(t)
	resetState(t, db)

	const (
		workers = 50
		amount  = 1_000_000
	)
	key := "idem-" + fmt.Sprint(time.Now().UnixNano())

	r := newReport(t, "50 concurrent requests — same idempotency key")
	r.input("workers", fmt.Sprintf("%d", workers))
	r.input("sender", "ACC-001")
	r.input("receiver", "ACC-002")
	r.input("amount", fmt.Sprintf("%d", amount))
	r.input("idempotency_key", key)

	fmt.Printf("\n=== TestIntegration_IdempotencyConcurrency ===\n")

	var wg sync.WaitGroup
	start := make(chan struct{})
	var serverErrors int64

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if status, _ := postTransfer(t, key, "ACC-001", "ACC-002", amount); status >= 500 {
				atomic.AddInt64(&serverErrors, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	r.checkInt("response.status_5xx", serverErrors, 0)
	r.checkInt("database.count_success_transfer",
		scalarInt(t, db, `SELECT COUNT(*) FROM transaction_transfers WHERE reference_id = $1`, key),
		1)
	for _, acc := range []struct {
		name string
		want int64
	}{
		{"ACC-001", seed["ACC-001"] - amount},
		{"ACC-002", seed["ACC-002"] + amount},
	} {
		label := "database.balance_%s"
		if bal, ok := queryBalance(db, acc.name); ok {
			r.checkInt(fmt.Sprintf(label, acc.name), bal, acc.want)
		} else {
			r.check(fmt.Sprintf(label, acc.name), "account not found in db", fmt.Sprintf("%d", acc.want))
		}
	}
	r.checkInt("database.count_created_ledger",
		scalarInt(t, db, `SELECT COUNT(*) FROM ledger_entries`),
		2)

	for _, acc := range []string{"ACC-001", "ACC-002"} {
		r.recordBalanceWithFlow(acc, seed[acc], db)
	}

	r.print()
}

// TestIntegration_HighConcurrencyConsistency drives many concurrent unique
// transfers and verifies the ledger stays perfectly balanced.
func TestIntegration_HighConcurrencyConsistency(t *testing.T) {
	checkServer(t)
	db := openDB(t)
	resetState(t, db)

	accounts := []string{"ACC-001", "ACC-002", "ACC-003"}
	const (
		workers = 500
		amount  = 3_000
	)

	// Top up ACC-003 so it can participate as a sender.
	if _, err := db.Exec(`UPDATE account_balances SET balance = 500000 WHERE account_number = 'ACC-003'`); err != nil {
		t.Fatalf("top up ACC-003: %v", err)
	}
	totalMoney := seed["ACC-001"] + seed["ACC-002"] + int64(500_000)

	fmt.Printf("\n=== TestIntegration_HighConcurrencyConsistency ===\n")
	r := newReport(t, fmt.Sprintf("%d concurrent unique transfers across 3 accounts", workers))
	r.input("workers", fmt.Sprintf("%d", workers))
	r.input("amount_each", fmt.Sprintf("%d", amount))
	r.input("accounts", strings.Join(accounts, ", "))
	r.input("total_money_in_system", fmt.Sprintf("%d", totalMoney))

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
			if status, _ := postTransfer(t, key, sender, receiver, amount); status >= 500 {
				atomic.AddInt64(&serverErrors, 1)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	r.checkInt("response.status_5xx", serverErrors, 0)

	seedOverride := map[string]int64{"ACC-003": 500_000}
	var totalActual int64
	for _, acc := range accounts {
		bal, ok := queryBalance(db, acc)
		if !ok {
			r.check(acc+".balance", "account not found in db", "exists")
			continue
		}
		if bal < 0 {
			r.check(acc+".balance_not_negative", fmt.Sprintf("%d", bal), ">= 0")
		}
		totalActual += bal
	}

	expectedFlow := int64(amount * workers)
	r.checkInt("system.credit_in_vs_moving_amount",
		scalarInt(t, db, `SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE entry_type = 'credit'`),
		expectedFlow)
	r.checkInt("system.debit_out_vs_moving_amount",
		scalarInt(t, db, `SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE entry_type = 'debit'`),
		expectedFlow)

	for _, acc := range accounts {
		delta := scalarInt(t, db, `
			SELECT COALESCE(SUM(CASE WHEN le.entry_type = 'credit' THEN le.amount ELSE -le.amount END), 0)
			FROM ledger_entries le
			JOIN account_balances ab ON ab.id = le.account_id
			WHERE ab.account_number = $1`, acc)
		base := seed[acc]
		if override, ok := seedOverride[acc]; ok {
			base = override
		}
		want := base + delta
		label := "database.balance_%s"
		if bal, ok := queryBalance(db, acc); ok {
			r.checkInt(fmt.Sprintf(label, acc)+"_ledger", bal, want)
		} else {
			r.check(fmt.Sprintf(label, acc)+"_ledger", "account not found in db", fmt.Sprintf("%d", want))
		}
	}

	for _, acc := range accounts {
		s := seed[acc]
		if override, ok := seedOverride[acc]; ok {
			s = override
		}
		r.recordBalanceWithFlow(acc, s, db)
	}

	r.print()
}

// TestIntegration_NoDeadlock_Transfers hammers the database with reciprocal
// A->B / B->A transfers simultaneously. ORDER BY on lock acquisition must
// prevent all deadlocks.
func TestIntegration_NoDeadlock_Transfers(t *testing.T) {
	checkServer(t)
	db := openDB(t)
	resetState(t, db)

	baseBalance := 100000000

	// Top up both accounts so neither runs dry — we want to exercise the lock
	// path for every request, not hit insufficient-balance errors.
	if _, err := db.Exec(`UPDATE account_balances SET balance = 100000000 WHERE account_number IN ('ACC-001','ACC-002')`); err != nil {
		t.Fatalf("top up: %v", err)
	}
	const workers = 300

	fmt.Printf("\n=== TestIntegration_NoDeadlock_Transfers ===\n")
	r := newReport(t, fmt.Sprintf("%d reciprocal pairs ACC-001<->ACC-002 (total %d goroutines)", workers, workers*2))
	r.input("pairs", fmt.Sprintf("%d", workers))
	r.input("amount_each", "100")
	r.input("balance.ACC-001", fmt.Sprint(baseBalance))
	r.input("balance.ACC-002", fmt.Sprint(baseBalance))

	var wg sync.WaitGroup
	start := make(chan struct{})
	var serverErrors int64

	fire := func(sender, receiver string, i int) {
		defer wg.Done()
		key := fmt.Sprintf("deadlock-%s-%d-%d", sender, time.Now().UnixNano(), i)
		<-start
		if status, _ := postTransfer(t, key, sender, receiver, 100); status >= 500 {
			atomic.AddInt64(&serverErrors, 1)
		}
	}

	done := make(chan struct{})
	for i := range workers {
		wg.Add(2)
		go fire("ACC-001", "ACC-002", i)
		go fire("ACC-002", "ACC-001", i)
	}
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

	var total int64
	accounts := []string{"ACC-001", "ACC-002"}
	for _, acc := range accounts {
		if bal, ok := queryBalance(db, acc); ok {
			total += bal
		} else {
			r.check(acc+".balance", "account not found in db", "exists")
		}
	}
	transfers := scalarInt(t, db, `SELECT COUNT(*) FROM transaction_transfers`)
	ledgers := scalarInt(t, db, `SELECT COUNT(*) FROM ledger_entries`)

	r.checkInt("response.status_5xx", serverErrors, 0)
	r.checkInt("database.count_created_ledger", ledgers, transfers*2)

	for _, acc := range accounts {
		delta := scalarInt(t, db, `
			SELECT COALESCE(SUM(CASE WHEN le.entry_type = 'credit' THEN le.amount ELSE -le.amount END), 0)
			FROM ledger_entries le
			JOIN account_balances ab ON ab.id = le.account_id
			WHERE ab.account_number = $1`, acc)

		want := int64(baseBalance) + delta
		label := "database.balance_%s"
		if bal, ok := queryBalance(db, acc); ok {
			r.checkInt(fmt.Sprintf(label, acc)+"_ledger", bal, want)
		} else {
			r.check(fmt.Sprintf(label, acc)+"_ledger", "account not found in db", fmt.Sprintf("%d", want))
		}
	}

	// Both accounts were topped up to 100_000_000 before the test.
	for _, acc := range []string{"ACC-001", "ACC-002"} {
		r.recordBalanceWithFlow(acc, 100_000_000, db)
	}

	r.print()
}
