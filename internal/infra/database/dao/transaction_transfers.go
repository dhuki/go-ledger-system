package dao

import (
	"database/sql"
	"time"
)

type TransactionTransfers struct {
	ID                   int64          `db:"id"`
	ReferenceID          string         `db:"reference_id"`
	SourceAccountID      int64          `db:"source_account_id"`
	DestinationAccountID int64          `db:"destination_account_id"`
	Amount               int64          `db:"amount"`
	Currency             string         `db:"currency"`
	Status               string         `db:"status"`
	Description          sql.NullString `db:"description"`
	CompletedAt          *time.Time     `db:"completed_at"`
	FailedAt             *time.Time     `db:"failed_at"`
	CreatedAt            time.Time      `db:"created_at"`
	UpdatedAt            time.Time      `db:"updated_at"`
}