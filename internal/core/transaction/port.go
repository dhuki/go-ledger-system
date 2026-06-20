package transaction

import (
	"context"
	"time"

	"github.com/dhuki/go-ledger-system/internal/adapter/http/model"
	"github.com/dhuki/go-ledger-system/internal/infra/database/repository/domain"
)

type Service interface {
	CreateTransfer(ctx context.Context, req *model.CreateTransferRequest) (*model.CreateTransferResponse, error)
}

type Repository interface {
	domain.TrxTransfer
	domain.Transaction
}

type Cache interface {
	SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error)
	Del(ctx context.Context, key string) error
}
