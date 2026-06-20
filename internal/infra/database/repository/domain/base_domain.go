package domain

import (
	"github.com/dhuki/go-ledger-system/internal/infra/database/repository/cqrs"
)

type DomainRepository interface {
	TrxAccountBalance
	TrxTransfer
	TrxLedgerEntry
	Transaction
	HealthCheck
}

type domainRepository struct {
	dbImpl cqrs.DB
}

func NewRepositoryDomain(db cqrs.DB) DomainRepository {
	return &domainRepository{dbImpl: db}
}
