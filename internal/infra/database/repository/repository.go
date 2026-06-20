package repository

import (
	"context"
	"database/sql"

	"github.com/dhuki/go-ledger-system/internal/infra/database/repository/cqrs"
)

type repository struct {
	dbImpl cqrs.DB
}

func NewQueryRepository(db *sql.DB) *repository {
	return &repository{dbImpl: db}
}

type transaction struct {
	tx *sql.Tx
}

func NewTransactionDB(tx *sql.Tx) cqrs.DB {
	return &transaction{tx: tx}
}

// --- Query ---

func (c *repository) PingContext(ctx context.Context) error {
	return c.dbImpl.PingContext(ctx)
}

func (c *repository) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.dbImpl.QueryContext(ctx, query, args...)
}

func (c *repository) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.dbImpl.QueryRowContext(ctx, query, args...)
}

func (c *repository) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return c.dbImpl.PrepareContext(ctx, query)
}

// PingContext is not meaningful inside a transaction; callers should ping before opening one.
func (c *transaction) PingContext(ctx context.Context) error {
	panic("unimplemented: ping inside transaction")
}

func (c *transaction) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.tx.QueryContext(ctx, query, args...)
}

func (c *transaction) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.tx.QueryRowContext(ctx, query, args...)
}

func (c *transaction) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return c.tx.PrepareContext(ctx, query)
}

// --- Command ---

func (c *repository) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return c.dbImpl.BeginTx(ctx, opts)
}

func (c *repository) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.dbImpl.ExecContext(ctx, query, args...)
}

// BeginTx is not supported inside an active transaction; nested transactions require savepoints.
func (c *transaction) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	panic("unimplemented: nested transactions not supported")
}

func (c *transaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.tx.ExecContext(ctx, query, args...)
}
