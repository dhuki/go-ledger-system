package dao

import "time"

type LedgerEntries struct {
	ID              int64     `db:"id"`
	TransactionID   int64     `db:"transaction_id"`
	AccountID       int64     `db:"account_id"`
	EntryType       string    `db:"entry_type"`
	Amount          int64     `db:"amount"`
	Currency        string    `db:"currency"`
	BalanceBefore   int64     `db:"balance_before"`
	BalanceAfter    int64     `db:"balance_after"`
	Description     string    `db:"description"`
	CreatedAt       time.Time `db:"created_at"`
}
