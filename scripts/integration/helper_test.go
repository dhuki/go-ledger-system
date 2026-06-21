package integration

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

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
// setup and assertions. It skips the test when DOCKER_PSQL_DSN is not set.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("DOCKER_PSQL_DSN")
	if dsn == "" {
		t.Skip("DOCKER_PSQL_DSN not set; skipping integration test")
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

// queryLedgerFlow returns the total credited and total debited for an account
// by summing its ledger entries. Both values are zero when no entries exist.
func queryLedgerFlow(db *sql.DB, account string) (creditIn, debitOut int64) {
	db.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN le.entry_type = 'credit' THEN le.amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN le.entry_type = 'debit'  THEN le.amount ELSE 0 END), 0)
		FROM ledger_entries le
		JOIN account_balances ab ON ab.id = le.account_id
		WHERE ab.account_number = $1`, account).Scan(&creditIn, &debitOut)
	return
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
		t.Fatalf("Failed to decode response: %v", err)
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
	balances     []balanceRow
}

type inputRow struct{ label, value string }
type checkRow struct {
	label string
	got   string
	want  string
	pass  bool
}
type balanceRow struct {
	account  string
	seed     int64
	actual   int64
	found    bool
	creditIn int64 // total credited via ledger; non-zero only when recorded with flow
	debitOut int64 // total debited via ledger; non-zero only when recorded with flow
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
	r.check("response.status", fmt.Sprintf("%d", got), fmt.Sprintf("%d", want))
}

func (r *caseReport) checkMessage(got, want string) {
	r.check("response.message", got, want)
}

// recordBalance snapshots the actual post-test balance for an account alongside
// its seed value so print() can display a before/after comparison.
func (r *caseReport) recordBalance(account string, seedAmt int64, db *sql.DB) {
	actual, found := queryBalance(db, account)
	r.balances = append(r.balances, balanceRow{
		account: account,
		seed:    seedAmt,
		actual:  actual,
		found:   found,
	})
}

// recordBalanceWithFlow is like recordBalance but also captures total credit and
// debit volume from the ledger. Use this when the transfer pattern is circular
// and net delta is expected to be zero — the flow columns show that transfers
// actually happened even though the balance did not change.
func (r *caseReport) recordBalanceWithFlow(account string, seedAmt int64, db *sql.DB) {
	actual, found := queryBalance(db, account)
	creditIn, debitOut := queryLedgerFlow(db, account)
	r.balances = append(r.balances, balanceRow{
		account:  account,
		seed:     seedAmt,
		actual:   actual,
		found:    found,
		creditIn: creditIn,
		debitOut: debitOut,
	})
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
			"response.status",
			fmt.Sprintf("%d", r.respStatus),
			func() string {
				for _, c := range r.checks {
					if c.label == "response.status" {
						return c.want
					}
				}
				return "-"
			}(),
		)
		fmt.Printf("    %-20s   got: %-30s  expected: %s\n",
			"response.body",
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

	if len(r.balances) > 0 {
		fmt.Println("  BALANCE STATE:")
		fmt.Printf("    %-12s  %-18s  %-18s  %s\n", "account", "seed", "actual", "delta")
		for _, b := range r.balances {
			if !b.found {
				fmt.Printf("    %-12s  %-18d  %-18s  %s\n", b.account, b.seed, "not found", "-")
				continue
			}
			fmt.Printf("    %-12s  %-18d  %-18d  %+d\n", b.account, b.seed, b.actual, b.actual-b.seed)
			if b.creditIn != 0 || b.debitOut != 0 {
				fmt.Printf("    %-12s  credit_in: %-7d  debit_out: %d\n", "", b.creditIn, b.debitOut)
			}
			fmt.Println()
		}
		fmt.Println()
	}

	fmt.Println("  VALIDATION:")
	fmt.Printf("    %-35s %-18s %-18s %s\n", "case", "got", "expected", "result")
	for _, c := range r.checks {
		fmt.Printf("    %-35s %-18s %-18s %s\n", c.label, c.got, c.want, verdict(c.pass))
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
