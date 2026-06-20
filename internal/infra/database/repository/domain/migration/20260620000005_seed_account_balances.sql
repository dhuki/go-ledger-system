-- +goose Up
-- +goose StatementBegin
INSERT INTO account_balances (account_number, holder_name, balance, currency, status)
VALUES
    ('ACC-001', 'Account 001', 10000000, 'IDR', 'active'),
    ('ACC-002', 'Account 002', 5000000, 'IDR', 'active'),
    ('ACC-003', 'Account 003', 0, 'IDR', 'active')
ON CONFLICT (account_number) DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM account_balances WHERE account_number IN ('ACC-001', 'ACC-002', 'ACC-003');
-- +goose StatementEnd
