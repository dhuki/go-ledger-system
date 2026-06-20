-- +goose Up
-- +goose StatementBegin
CREATE INDEX idx_account_id_ledger_entries ON ledger_entries(account_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_account_id_ledger_entries;
-- +goose StatementEnd
