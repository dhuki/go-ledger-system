package dao

import "time"

type AccountBalances struct {
	ID            int64     `db:"id"`
	AccountNumber string    `db:"account_number"`
	HolderName    string    `db:"holder_name"`
	Balance       int64     `db:"balance"`
	Currency      string    `db:"currency"`
	Status        string    `db:"status"`
	CreatedAt     time.Time `db:"created_at"`
	UpdatedAt     time.Time `db:"updated_at"`
}

type UpdateAccountParams struct {
	ID      int64
	Balance *int64
	Status  *string
}
