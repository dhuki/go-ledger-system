package domain

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/dhuki/go-ledger-system/internal/infra/database/repository"
)

type Transaction interface {
	Transaction(ctx context.Context, fn func(tx DomainRepository) error) error
}

func (m *domainRepository) Transaction(ctx context.Context, fn func(tx DomainRepository) error) error {
	tx, err := m.dbImpl.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	txClient := &domainRepository{dbImpl: repository.NewTransactionDB(tx)}
	if err := fn(txClient); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("rollback transaction: %v, original error: %w", rollbackErr, err)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
