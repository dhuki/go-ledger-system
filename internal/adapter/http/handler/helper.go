package handler

import (
	"errors"
	"net/http"

	transfer "github.com/dhuki/go-ledger-system/internal/core/transaction"
)

func mapErrorToStatus(err error) int {
	switch {
	case errors.Is(err, transfer.ErrMissingIdempotencyKey),
		errors.Is(err, transfer.ErrInvalidAmount),
		errors.Is(err, transfer.ErrSelfTransfer),
		errors.Is(err, transfer.ErrTranferStillProcessing):
		return http.StatusBadRequest
	case errors.Is(err, transfer.ErrAccountNotFound):
		return http.StatusNotFound
	case errors.Is(err, transfer.ErrInsufficientBalance):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}
