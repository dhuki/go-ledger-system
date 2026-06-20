package transaction

import (
	"context"
	"fmt"
	"time"

	"github.com/dhuki/go-ledger-system/internal/adapter/http/middleware"
	httpModel "github.com/dhuki/go-ledger-system/internal/adapter/http/model"
	"github.com/dhuki/go-ledger-system/internal/infra/configloader"
	"github.com/dhuki/go-ledger-system/internal/infra/database/dao"
	"github.com/dhuki/go-ledger-system/internal/infra/database/repository/domain"
	"github.com/dhuki/go-ledger-system/internal/infra/logger"
)

type service struct {
	repo  Repository
	cache Cache
}

func NewService(repo Repository, cache Cache) Service {
	return &service{
		repo:  repo,
		cache: cache,
	}
}

func (s *service) CreateTransfer(ctx context.Context, req *httpModel.CreateTransferRequest) (*httpModel.CreateTransferResponse, error) {
	idempotencyVal, ok := ctx.Value(middleware.IdempotencyKey).(string)
	if !ok || idempotencyVal == "" {
		return nil, ErrMissingIdempotencyKey
	}

	// SetNX claims the idempotency key in cache. Only the request that wins the
	// claim proceeds to the database; all others are blocked here upfront.
	idemptencyKey := fmt.Sprintf(IdempotencyRequestKey, idempotencyVal)
	set, err := s.cache.SetNX(ctx, idemptencyKey, idempotencyVal, configloader.GetConfig("redis_transfer_idempotency_ttl").GetDuration())
	if err != nil {
		logger.Error(ctx, "Failed to SetNX", err)
		return nil, err
	}
	defer s.cache.Del(ctx, idemptencyKey)

	if !set {
		// Key already claimed: either a completed replay or a concurrent duplicate.
		existing, err := s.repo.GetTransferByReferenceID(ctx, idempotencyVal)
		if err != nil {
			logger.Error(ctx, "Failed to GetTransferByReferenceID", err)
			return nil, err
		}
		if existing != nil {
			return &httpModel.CreateTransferResponse{TransactionID: existing.ID}, nil
		}
		// In-flight: the first request holds the key but hasn't committed yet.
		return nil, ErrTranferStillProcessing
	}

	var createdTransferID int64
	now := time.Now().UTC()
	err = s.repo.Transaction(ctx, func(tx domain.DomainRepository) error {
		// Lock both accounts in a deterministic global order (sorted by account
		// number) so reciprocal transfers (A->B and B->A) can never deadlock.
		sender, receiver, err := s.lockAccountsOrdered(ctx, tx, req.SenderAccount, req.ReceiverAccount)
		if err != nil {
			return err
		}
		if sender == nil || receiver == nil {
			return ErrAccountNotFound
		}
		if sender.Balance < req.Amount {
			return ErrInsufficientBalance
		}

		// Safety re-check under the row lock: guards against the edge case where
		// the SetNX TTL expired between claim and commit and another request
		// already completed the transfer.
		existing, err := tx.GetTransferByReferenceID(ctx, idempotencyVal)
		if err != nil {
			logger.Error(ctx, "Failed to GetTransferByReferenceID", err)
			return err
		}
		if existing != nil {
			createdTransferID = existing.ID
			return nil
		}

		senderBalance := sender.Balance - req.Amount
		receiverBalance := receiver.Balance + req.Amount

		if err := tx.UpdateAccountBalance(ctx, dao.UpdateAccountParams{ID: sender.ID, Balance: &senderBalance}); err != nil {
			logger.Error(ctx, "Failed to UpdateAccountBalance", err)
			return err
		}
		if err := tx.UpdateAccountBalance(ctx, dao.UpdateAccountParams{ID: receiver.ID, Balance: &receiverBalance}); err != nil {
			logger.Error(ctx, "Failed to UpdateAccountBalance", err)
			return err
		}

		transferID, err := tx.InsertTransfer(ctx, dao.TransactionTransfers{
			ReferenceID:          idempotencyVal,
			SourceAccountID:      sender.ID,
			DestinationAccountID: receiver.ID,
			Amount:               req.Amount,
			Currency:             CurrencyIDR,
			Status:               StatusCompleted,
			CompletedAt:          &now,
			CreatedAt:            now,
		})
		if err != nil {
			logger.Error(ctx, "Failed to InsertTransfer", err)
			return err
		}
		createdTransferID = transferID

		if err := tx.InsertLedgerEntriesBulk(ctx, []dao.LedgerEntries{
			{
				TransactionID: transferID,
				AccountID:     sender.ID,
				EntryType:     EntryTypeDebit,
				Amount:        req.Amount,
				Currency:      CurrencyIDR,
				BalanceBefore: sender.Balance,
				BalanceAfter:  senderBalance,
			},
			{
				TransactionID: transferID,
				AccountID:     receiver.ID,
				EntryType:     EntryTypeCredit,
				Amount:        req.Amount,
				Currency:      CurrencyIDR,
				BalanceBefore: receiver.Balance,
				BalanceAfter:  receiverBalance,
			},
		}); err != nil {
			logger.Error(ctx, "Failed to InsertLedgerEntriesBulk", err)
			return err
		}

		return nil
	})
	if err != nil {
		logger.Error(ctx, "Failed to CreateTransfer", err)
		return nil, err
	}

	return &httpModel.CreateTransferResponse{
		TransactionID: createdTransferID,
	}, nil
}

// lockAccountsOrdered locks both accounts in a single query ordered by account
// number. ORDER BY guarantees every transaction acquires locks in the same
// sequence regardless of which direction the transfer runs, preventing deadlocks.
func (s *service) lockAccountsOrdered(ctx context.Context, tx domain.DomainRepository, senderAccount, receiverAccount string) (sender, receiver *dao.AccountBalances, err error) {
	accounts, err := tx.GetAccountsForUpdate(ctx, []string{senderAccount, receiverAccount})
	if err != nil {
		logger.Error(ctx, "Failed to GetAccountsForUpdate", err)
		return nil, nil, err
	}

	accMap := make(map[string]*dao.AccountBalances, len(accounts))
	for _, acc := range accounts {
		accMap[acc.AccountNumber] = acc
	}
	return accMap[senderAccount], accMap[receiverAccount], nil
}
