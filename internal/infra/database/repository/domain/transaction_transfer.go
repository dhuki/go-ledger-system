package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/dhuki/go-ledger-system/internal/infra/database/dao"
	"github.com/dhuki/go-ledger-system/internal/infra/logger"
)

type TrxTransfer interface {
	GetTransferByReferenceID(ctx context.Context, referenceID string) (*dao.TransactionTransfers, error)
	InsertTransfer(ctx context.Context, transfer dao.TransactionTransfers) (int64, error)
}

func (r *domainRepository) GetTransferByReferenceID(ctx context.Context, referenceID string) (*dao.TransactionTransfers, error) {
	row := r.dbImpl.QueryRowContext(ctx, `
		SELECT id, reference_id, source_account_id, destination_account_id, amount, currency,
		       status, description, completed_at, failed_at, created_at, updated_at
		FROM transaction_transfers
		WHERE reference_id = $1`, referenceID)

	var transfer dao.TransactionTransfers
	if err := row.Scan(
		&transfer.ID, &transfer.ReferenceID, &transfer.SourceAccountID, &transfer.DestinationAccountID,
		&transfer.Amount, &transfer.Currency, &transfer.Status, &transfer.Description,
		&transfer.CompletedAt, &transfer.FailedAt, &transfer.CreatedAt, &transfer.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		logger.Error(ctx, "Failed to GetTransferByReferenceID", err)
		return nil, fmt.Errorf("scan transfer %s: %w", referenceID, err)
	}
	return &transfer, nil
}

func (r *domainRepository) InsertTransfer(ctx context.Context, transfer dao.TransactionTransfers) (int64, error) {
	var id int64
	err := r.dbImpl.QueryRowContext(ctx, `
		INSERT INTO transaction_transfers
			(reference_id, source_account_id, destination_account_id, amount, currency, status, completed_at, failed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		transfer.ReferenceID, transfer.SourceAccountID, transfer.DestinationAccountID,
		transfer.Amount, transfer.Currency, transfer.Status, transfer.CompletedAt, transfer.FailedAt,
	).Scan(&id)
	if err != nil {
		logger.Error(ctx, "Failed to InsertTransfer", err)
		return 0, fmt.Errorf("insert transfer %s: %w", transfer.ReferenceID, err)
	}
	return id, nil
}
