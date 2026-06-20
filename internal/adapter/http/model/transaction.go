package model

import "errors"

var (
	ErrInvalidAmount = errors.New("amount must be greater than zero")
	ErrSelfTransfer  = errors.New("cannot transfer to the same account")
)

type CreateTransferRequest struct {
	SenderAccount   string `json:"sender_account"`
	ReceiverAccount string `json:"receiver_account"`
	Amount          int64  `json:"amount"`
}

func (c *CreateTransferRequest) Validate() error {
	if c.Amount <= 0 {
		return ErrInvalidAmount
	}
	if c.SenderAccount == c.ReceiverAccount {
		return ErrSelfTransfer
	}

	return nil
}

type CreateTransferResponse struct {
	TransactionID int64 `json:"transaction_id"`
}
