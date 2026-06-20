package integration

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/dhuki/go-ledger-system/internal/adapter/http/model"
	"github.com/google/uuid"
)

const (
	baseUrl      = "http://localhost:8080"
	transferPath = "/api/v1/transfer"
)

// seed is the known-good balance for each account before every test.
var (
	seed = map[string]int64{
		"ACC-001": 10_000_000,
		"ACC-002": 5_000_000,
		"ACC-003": 0,
	}

	baseResponse model.BaseResponse
)

// ----------------------------------------------------------------------------
// Infrastructure helpers
// ----------------------------------------------------------------------------

// openDB opens a direct PostgreSQL connection to the docker database for test
// setup and assertions. It skips the test when LEDGER_TEST_DSN is not set.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("LEDGER_TEST_DSN")
	if dsn == "" {
		t.Skip("LEDGER_TEST_DSN not set; skipping integration test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// checkServer skips the test when the service at baseUrl is not reachable.
func checkServer(t *testing.T) {
	t.Helper()
	resp, err := http.Get(baseUrl + "/api/v1/health")
	if err != nil || resp.StatusCode >= 500 {
		t.Skipf("service at %s not reachable; skipping integration test", baseUrl)
	}
	resp.Body.Close()
}

// resetState truncates all transactional tables and re-seeds the three accounts
// to their known-good balances. Called at the start of every test.
func resetState(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`TRUNCATE ledger_entries, transaction_transfers RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	for number, balance := range seed {
		if _, err := db.Exec(
			`UPDATE account_balances SET balance = $1 WHERE account_number = $2`,
			balance, number,
		); err != nil {
			t.Fatalf("reset balance %s: %v", number, err)
		}
	}
}

// queryBalance returns the balance for an account and false when the account
// does not exist, letting callers record a FAIL check and still reach r.print().
func queryBalance(db *sql.DB, account string) (int64, bool) {
	var balance int64
	err := db.QueryRow(`SELECT balance FROM account_balances WHERE account_number = $1`, account).Scan(&balance)
	if err != nil {
		return 0, false
	}
	return balance, true
}

func scalarInt(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("scalarInt %q: %v", query, err)
	}
	return n
}

// ----------------------------------------------------------------------------
// HTTP helper
// ----------------------------------------------------------------------------

// postTransfer sends one transfer request and returns the HTTP status code and
// the raw response body so callers can include it in the test report.
func postTransfer(t *testing.T, idempotencyKey, sender, receiver string, amount int64) (int, model.BaseResponse) {
	t.Helper()

	reqBody, err := json.Marshal(model.CreateTransferRequest{
		SenderAccount:   sender,
		ReceiverAccount: receiver,
		Amount:          amount,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s%s", baseUrl, transferPath), bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("Failed to Create Request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trace-ID", uuid.NewString())
	if idempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", idempotencyKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to Do request: %v", err)
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&baseResponse); err != nil {
		t.Fatalf("Failed to decode rseponse: %v", err)
	}

	return resp.StatusCode, baseResponse
}

// ----------------------------------------------------------------------------
// Report helpers
// ----------------------------------------------------------------------------

// caseReport collects inputs, the raw HTTP response, and validation checks for
// a single test case, then prints a structured summary to stdout.
type caseReport struct {
	t            *testing.T
	name         string
	inputs       []inputRow
	respStatus   int
	respBody     model.BaseResponse
	wantRespBody string // optional expected body snippet for display
	checks       []checkRow
}

type inputRow struct{ label, value string }
type checkRow struct {
	label string
	got   string
	want  string
	pass  bool
}

func newReport(t *testing.T, name string) *caseReport {
	t.Helper()
	return &caseReport{t: t, name: name}
}

func (r *caseReport) input(label, value string) {
	r.inputs = append(r.inputs, inputRow{label, value})
}

// setResponse records the raw HTTP status and body returned by the server.
func (r *caseReport) setResponse(status int, body model.BaseResponse) {
	r.respStatus = status
	r.respBody = body
}

// expectResponse sets the body string shown in the EXPECTED column of the
// response section. Use a representative snippet, not an exact match.
func (r *caseReport) expectResponse(body string) {
	r.wantRespBody = body
}

func (r *caseReport) check(label, got, want string) {
	pass := got == want
	r.checks = append(r.checks, checkRow{label: label, got: got, want: want, pass: pass})
	if !pass {
		r.t.Fail()
	}
}

func (r *caseReport) checkInt(label string, got, want int64) {
	r.check(label, fmt.Sprintf("%d", got), fmt.Sprintf("%d", want))
}

func (r *caseReport) checkHTTP(got, want int) {
	r.check("http_status", fmt.Sprintf("%d", got), fmt.Sprintf("%d", want))
}

func (r *caseReport) checkMessage(got, want string) {
	r.check("response.message", got, want)
}

func (r *caseReport) print() {
	const w = 68
	sep := strings.Repeat("-", w)
	verdict := func(pass bool) string {
		if pass {
			return "PASS"
		}
		return "FAIL"
	}

	allPass := true
	for _, c := range r.checks {
		if !c.pass {
			allPass = false
			break
		}
	}

	fmt.Println(sep)
	fmt.Printf("CASE: %s\n", r.name)
	fmt.Println()

	fmt.Println("  INPUT:")
	for _, in := range r.inputs {
		fmt.Printf("    %-20s = %s\n", in.label, in.value)
	}
	fmt.Println()

	if r.respBody.Message != "" || r.respStatus != 0 {
		fmt.Println("  RESPONSE:")
		fmt.Printf("    %-20s   got: %-30s  expected: %s\n",
			"http_status",
			fmt.Sprintf("%d", r.respStatus),
			func() string {
				for _, c := range r.checks {
					if c.label == "http_status" {
						return c.want
					}
				}
				return "-"
			}(),
		)
		fmt.Printf("    %-20s   got: %-30s  expected: %s\n",
			"body",
			truncate(r.respBody.Message, 50),
			func() string {
				if r.wantRespBody != "" {
					return r.wantRespBody
				}
				return "-"
			}(),
		)
		fmt.Println()
	}

	fmt.Println("  VALIDATION:")
	fmt.Printf("    %-32s %-18s %-18s %s\n", "case", "got", "expected", "result")
	for _, c := range r.checks {
		fmt.Printf("    %-32s %-18s %-18s %s\n", c.label, c.got, c.want, verdict(c.pass))
	}
	fmt.Println()
	fmt.Printf("  OVERALL: %s\n", verdict(allPass))
	fmt.Println(sep)
}

// truncate shortens s to at most n runes, appending "…" when cut.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

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
		name            string
		sender          string
		receiver        string
		amount          int64
		wantStatus      int
		wantMessage     string // expected BaseResponse.Message
		wantTransfers   int64
		wantSenderInDB  bool   // false = sender must not exist in DB
		wantSenderBal   int64  // only checked when wantSenderInDB == true
		wantReceiverBal string // account number to assert
		wantRecBal      int64
	}{
		{
			name:            "zero amount transfer",
			sender:          "ACC-001", receiver: "ACC-002", amount: 0,
			wantStatus:      http.StatusBadRequest,
			wantMessage:     "amount must be greater than zero",
			wantTransfers:   0,
			wantSenderInDB:  true,
			wantSenderBal:   seed["ACC-001"], wantReceiverBal: "ACC-002", wantRecBal: seed["ACC-002"],
		},
		{
			name:            "self transfer",
			sender:          "ACC-001", receiver: "ACC-001", amount: 100,
			wantStatus:      http.StatusBadRequest,
			wantMessage:     "cannot transfer to the same account",
			wantTransfers:   0,
			wantSenderInDB:  true,
			wantSenderBal:   seed["ACC-001"], wantReceiverBal: "ACC-001", wantRecBal: seed["ACC-001"],
		},
		{
			name:            "sender not found",
			sender:          "ACC-999", receiver: "ACC-002", amount: 100,
			wantStatus:      http.StatusNotFound,
			wantMessage:     "account not found",
			wantTransfers:   0,
			wantSenderInDB:  false, // ACC-999 must not exist in DB — not-found is the PASS expectation
			wantReceiverBal: "ACC-002", wantRecBal: seed["ACC-002"],
		},
		{
			name:            "insufficient balance",
			sender:          "ACC-003", receiver: "ACC-001", amount: 1,
			wantStatus:      http.StatusUnprocessableEntity,
			wantMessage:     "insufficient balance",
			wantTransfers:   0,
			wantSenderInDB:  true,
			wantSenderBal:   seed["ACC-003"], wantReceiverBal: "ACC-001", wantRecBal: seed["ACC-001"],
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
		r.checkInt("transfers_in_db",
			scalarInt(t, db, `SELECT COUNT(*) FROM transaction_transfers`),
			tc.wantTransfers)
		r.checkInt("ledger_entries_in_db",
			scalarInt(t, db, `SELECT COUNT(*) FROM ledger_entries`),
			0)

		if tc.wantSenderInDB {
			if bal, ok := queryBalance(db, tc.sender); ok {
				r.checkInt(tc.sender+".balance", bal, tc.wantSenderBal)
			} else {
				r.check(tc.sender+".balance", "not found in db", fmt.Sprintf("%d", tc.wantSenderBal))
			}
		} else {
			_, ok := queryBalance(db, tc.sender)
			got := "not found in db"
			if ok {
				got = "found in db"
			}
			r.check(tc.sender+".exists_in_db", got, "not found in db")
		}
		if tc.wantReceiverBal != "" {
			if bal, ok := queryBalance(db, tc.wantReceiverBal); ok {
				r.checkInt(tc.wantReceiverBal+".balance", bal, tc.wantRecBal)
			} else {
				r.check(tc.wantReceiverBal+".balance", "not found in db", fmt.Sprintf("%d", tc.wantRecBal))
			}
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

	r.checkInt("5xx_responses", serverErrors, 0)
	r.checkInt("transfers_in_db",
		scalarInt(t, db, `SELECT COUNT(*) FROM transaction_transfers WHERE reference_id = $1`, key),
		1)
	for _, acc := range []struct {
		name string
		want int64
	}{
		{"ACC-001", seed["ACC-001"] - amount},
		{"ACC-002", seed["ACC-002"] + amount},
	} {
		if bal, ok := queryBalance(db, acc.name); ok {
			r.checkInt(acc.name+".balance", bal, acc.want)
		} else {
			r.check(acc.name+".balance", "account not found in db", fmt.Sprintf("%d", acc.want))
		}
	}
	r.checkInt("ledger_entries_in_db",
		scalarInt(t, db, `SELECT COUNT(*) FROM ledger_entries`),
		2)

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
		workers = 300
		amount  = 1_000
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

	r.checkInt("5xx_responses", serverErrors, 0)

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
	r.checkInt("total_money_conserved", totalActual, totalMoney)

	transfers := scalarInt(t, db, `SELECT COUNT(*) FROM transaction_transfers`)
	ledgers := scalarInt(t, db, `SELECT COUNT(*) FROM ledger_entries`)
	r.checkInt("ledger_entries_per_transfer (ratio*transfers)", ledgers, transfers*2)

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
		if bal, ok := queryBalance(db, acc); ok {
			r.checkInt(acc+".balance_matches_ledger", bal, want)
		} else {
			r.check(acc+".balance_matches_ledger", "account not found in db", fmt.Sprintf("%d", want))
		}
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

	// Top up both accounts so neither runs dry — we want to exercise the lock
	// path for every request, not hit insufficient-balance errors.
	if _, err := db.Exec(`UPDATE account_balances SET balance = 100000000 WHERE account_number IN ('ACC-001','ACC-002')`); err != nil {
		t.Fatalf("top up: %v", err)
	}
	const pairs = 150

	fmt.Printf("\n=== TestIntegration_NoDeadlock_Transfers ===\n")
	r := newReport(t, fmt.Sprintf("%d reciprocal pairs ACC-001<->ACC-002 (total %d goroutines)", pairs, pairs*2))
	r.input("pairs", fmt.Sprintf("%d", pairs))
	r.input("amount_each", "100")
	r.input("ACC-001.balance_before", "100000000")
	r.input("ACC-002.balance_before", "100000000")

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
	for i := range pairs {
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
	for _, acc := range []string{"ACC-001", "ACC-002"} {
		if bal, ok := queryBalance(db, acc); ok {
			total += bal
		} else {
			r.check(acc+".balance", "account not found in db", "exists")
		}
	}
	transfers := scalarInt(t, db, `SELECT COUNT(*) FROM transaction_transfers`)
	ledgers := scalarInt(t, db, `SELECT COUNT(*) FROM ledger_entries`)

	r.checkInt("5xx_responses", serverErrors, 0)
	r.checkInt("total_money_conserved", total, 200_000_000)
	r.checkInt("ledger_entries_per_transfer (ratio*transfers)", ledgers, transfers*2)

	r.print()
}
