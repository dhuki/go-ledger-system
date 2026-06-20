package transaction

import (
	"errors"
)

const (
	StatusCompleted = "completed"

	EntryTypeDebit  = "debit"
	EntryTypeCredit = "credit"

	CurrencyIDR = "IDR"
)

var (
	IdempotencyRequestKey = "transfer:idempotency:request:%s"
)

var (
	ErrMissingIdempotencyKey  = errors.New("idempotency key is required")
	ErrTranferStillProcessing = errors.New("transfer request still processing")
	ErrInvalidAmount          = errors.New("amount must be greater than zero")
	ErrSelfTransfer           = errors.New("cannot transfer to the same account")
	ErrAccountNotFound        = errors.New("account not found")
	ErrInsufficientBalance    = errors.New("insufficient balance")
)
