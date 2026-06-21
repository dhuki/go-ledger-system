package domain

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/dhuki/go-ledger-system/internal/infra/database/dao"
	"github.com/dhuki/go-ledger-system/internal/infra/logger"
)

type TrxAccountBalance interface {
	GetAccountsForUpdate(ctx context.Context, accountNumbers []string) ([]*dao.AccountBalances, error)
	UpdateAccountBalance(ctx context.Context, params dao.UpdateAccountParams) error
}

func (r *domainRepository) GetAccountsForUpdate(ctx context.Context, accountNumbers []string) ([]*dao.AccountBalances, error) {
	if len(accountNumbers) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(accountNumbers))
	args := make([]any, len(accountNumbers))
	for i, n := range accountNumbers {
		placeholders[i] = "$" + strconv.Itoa(i+1)
		args[i] = n
	}

	query := `
		SELECT id, account_number, holder_name, balance, currency, status, created_at, updated_at
		FROM account_balances
		WHERE account_number IN (` + strings.Join(placeholders, ", ") + `)
		ORDER BY account_number
		FOR UPDATE`

	rows, err := r.dbImpl.QueryContext(ctx, query, args...)
	if err != nil {
		logger.Error(ctx, "Failed to GetAccountsForUpdate", err)
		return nil, err
	}
	defer rows.Close()

	var accounts []*dao.AccountBalances
	for rows.Next() {
		var account dao.AccountBalances
		if err := rows.Scan(
			&account.ID, &account.AccountNumber, &account.HolderName, &account.Balance,
			&account.Currency, &account.Status,
			&account.CreatedAt, &account.UpdatedAt,
		); err != nil {
			logger.Error(ctx, "Failed to GetAccountsForUpdate", err)
			return nil, err
		}
		accounts = append(accounts, &account)
	}
	if err := rows.Err(); err != nil {
		logger.Error(ctx, "Failed to GetAccountsForUpdate", err)
		return nil, err
	}
	return accounts, nil
}

func (r *domainRepository) UpdateAccountBalance(ctx context.Context, params dao.UpdateAccountParams) error {
	var setClauses []string
	var args []any

	if params.Balance != nil {
		args = append(args, *params.Balance)
		setClauses = append(setClauses, "balance = $"+strconv.Itoa(len(args)))
	}
	if params.Status != nil {
		args = append(args, *params.Status)
		setClauses = append(setClauses, "status = $"+strconv.Itoa(len(args)))
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, params.ID)

	query := fmt.Sprintf(
		"UPDATE account_balances SET %s WHERE id = $%d",
		strings.Join(setClauses, ", "),
		len(args),
	)

	if _, err := r.dbImpl.ExecContext(ctx, query, args...); err != nil {
		logger.Error(ctx, "Failed to UpdateAccountBalance", err)
		return err
	}
	return nil
}
