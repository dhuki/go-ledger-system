-- +goose Up
-- +goose StatementBegin
CREATE TABLE transaction_transfers (
    id                      BIGSERIAL PRIMARY KEY,
    reference_id            VARCHAR(255) NOT NULL UNIQUE,
    source_account_id       BIGINT NOT NULL REFERENCES account_balances(id),
    destination_account_id  BIGINT NOT NULL REFERENCES account_balances(id),
    amount                  BIGINT NOT NULL,
    currency                VARCHAR(10) NOT NULL DEFAULT 'IDR',
    status                  VARCHAR(20) NOT NULL,
    description             TEXT,
    completed_at            TIMESTAMPTZ,
    failed_at               TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_transfer_amount_positive CHECK (amount > 0)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE transaction_transfers;
-- +goose StatementEnd
