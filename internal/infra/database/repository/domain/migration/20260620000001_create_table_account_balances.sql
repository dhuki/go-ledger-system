-- +goose Up
-- +goose StatementBegin
CREATE TABLE account_balances (
    id              BIGSERIAL PRIMARY KEY,
    account_number  VARCHAR(50) NOT NULL UNIQUE,
    holder_name     VARCHAR(255) NOT NULL DEFAULT '',
    balance         BIGINT NOT NULL DEFAULT 0,
    currency        VARCHAR(10) NOT NULL DEFAULT 'IDR',
    status          VARCHAR(20) NOT NULL DEFAULT 'active',
    frozen_at       TIMESTAMPTZ,
    closed_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_balance_non_negative CHECK (balance >= 0)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE account_balances;
-- +goose StatementEnd
