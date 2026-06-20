-- +goose Up
-- +goose StatementBegin
CREATE INDEX idx_transaction_id_ledger_entries ON ledger_entries(transaction_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_transaction_id_ledger_entries;
-- +goose StatementEnd
