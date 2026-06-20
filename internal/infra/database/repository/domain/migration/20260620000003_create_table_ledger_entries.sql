-- +goose Up
-- +goose StatementBegin
CREATE TABLE ledger_entries (
    id              BIGSERIAL PRIMARY KEY,
    transaction_id  BIGINT NOT NULL REFERENCES transaction_transfers(id),
    account_id      BIGINT NOT NULL REFERENCES account_balances(id),
    entry_type      VARCHAR(10) NOT NULL,
    amount          BIGINT NOT NULL,
    currency        VARCHAR(10) NOT NULL DEFAULT 'IDR',
    balance_before  BIGINT NOT NULL,
    balance_after   BIGINT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_entry_type CHECK (entry_type IN ('debit', 'credit'))
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE ledger_entries;
-- +goose StatementEnd
