package domain

import (
	"context"
	"fmt"
	"strings"

	"github.com/dhuki/go-ledger-system/internal/infra/database/dao"
	"github.com/dhuki/go-ledger-system/internal/infra/logger"
)

type TrxLedgerEntry interface {
	InsertLedgerEntriesBulk(ctx context.Context, entries []dao.LedgerEntries) error
}

func (r *domainRepository) InsertLedgerEntriesBulk(ctx context.Context, entries []dao.LedgerEntries) error {
	if len(entries) == 0 {
		return nil
	}

	var placeholders []string
	var args []any
	for _, entry := range entries {
		n := len(args)
		placeholders = append(placeholders, fmt.Sprintf(
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			n+1, n+2, n+3, n+4, n+5, n+6, n+7, n+8,
		))
		args = append(args,
			entry.TransactionID, entry.AccountID, entry.EntryType, entry.Amount,
			entry.Currency, entry.BalanceBefore, entry.BalanceAfter, entry.Description,
		)
	}

	query := `
		INSERT INTO ledger_entries
			(transaction_id, account_id, entry_type, amount, currency, balance_before, balance_after, description)
		VALUES ` + strings.Join(placeholders, ", ")

	if _, err := r.dbImpl.ExecContext(ctx, query, args...); err != nil {
		logger.Error(ctx, "Failed to InsertLedgerEntriesBulk", err)
		return fmt.Errorf("insert ledger entries for transaction %d: %w", entries[0].TransactionID, err)
	}
	return nil
}
