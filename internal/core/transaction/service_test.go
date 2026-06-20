package transaction

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/dhuki/go-ledger-system/internal/adapter/http/middleware"
	"github.com/dhuki/go-ledger-system/internal/adapter/http/model"
	txmocks "github.com/dhuki/go-ledger-system/internal/core/transaction/mocks"
	"github.com/dhuki/go-ledger-system/internal/infra/database/dao"
	"github.com/dhuki/go-ledger-system/internal/infra/database/repository/domain"
	domainmock "github.com/dhuki/go-ledger-system/internal/infra/database/repository/domain/mock"
)

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func ctxWithKey(key string) context.Context {
	return context.WithValue(context.Background(), middleware.IdempotencyKey, key)
}

func transferReq(sender, receiver string, amount int64) *model.CreateTransferRequest {
	return &model.CreateTransferRequest{
		SenderAccount:   sender,
		ReceiverAccount: receiver,
		Amount:          amount,
	}
}

// newSuccessCacheMock sets up MockCache for a request that wins the SetNX
// claim (returns true) and releases the key via Del on defer.
func newSuccessCacheMock(t *testing.T, idemKey string) *txmocks.MockCache {
	t.Helper()
	cacheKey := fmt.Sprintf(IdempotencyRequestKey, idemKey)
	c := txmocks.NewMockCache(t)
	c.EXPECT().SetNX(mock.Anything, cacheKey, idemKey, mock.Anything).Return(true, nil).Once()
	c.EXPECT().Del(mock.Anything, cacheKey).Return(nil).Once()
	return c
}

// ----------------------------------------------------------------------------
// TestCreateTransfer_HappyPath
// ----------------------------------------------------------------------------

func TestCreateTransfer_HappyPath(t *testing.T) {
	tests := []struct {
		name            string
		senderAccount   string
		receiverAccount string
		senderBalance   int64
		receiverBalance int64
		amount          int64
		wantSenderBal   int64
		wantReceiverBal int64
	}{
		{
			name:          "transfers amount between accounts",
			senderAccount: "ACC-A", receiverAccount: "ACC-B",
			senderBalance: 10_000_000, receiverBalance: 5_000_000,
			amount:        1_000_000,
			wantSenderBal: 9_000_000, wantReceiverBal: 6_000_000,
		},
		{
			name:          "drains sender balance to zero",
			senderAccount: "ACC-A", receiverAccount: "ACC-B",
			senderBalance: 500, receiverBalance: 0,
			amount:        500,
			wantSenderBal: 0, wantReceiverBal: 500,
		},
		{
			name:          "receiver already has funds",
			senderAccount: "ACC-A", receiverAccount: "ACC-B",
			senderBalance: 2_000_000, receiverBalance: 8_000_000,
			amount:        2_000_000,
			wantSenderBal: 0, wantReceiverBal: 10_000_000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idemKey := uuid.NewString()
			sender := &dao.AccountBalances{ID: 1, AccountNumber: tt.senderAccount, Balance: tt.senderBalance}
			receiver := &dao.AccountBalances{ID: 2, AccountNumber: tt.receiverAccount, Balance: tt.receiverBalance}

			mockDomain := domainmock.NewMockDomainRepository(t)
			mockDomain.EXPECT().GetAccountsForUpdate(mock.Anything, mock.Anything).
				Return([]*dao.AccountBalances{sender, receiver}, nil).Once()
			mockDomain.EXPECT().GetTransferByReferenceID(mock.Anything, idemKey).
				Return(nil, nil).Once()

			var capturedUpdates []dao.UpdateAccountParams
			mockDomain.EXPECT().UpdateAccountBalance(mock.Anything, mock.Anything).
				RunAndReturn(func(_ context.Context, p dao.UpdateAccountParams) error {
					capturedUpdates = append(capturedUpdates, p)
					return nil
				}).Times(2)

			mockDomain.EXPECT().InsertTransfer(mock.Anything, mock.Anything).Return(int64(1), nil).Once()
			mockDomain.EXPECT().InsertLedgerEntriesBulk(mock.Anything, mock.Anything).Return(nil).Once()

			mockRepo := txmocks.NewMockRepository(t)
			mockRepo.EXPECT().Transaction(mock.Anything, mock.Anything).
				RunAndReturn(func(ctx context.Context, fn func(domain.DomainRepository) error) error {
					return fn(mockDomain)
				}).Once()

			svc := NewService(mockRepo, newSuccessCacheMock(t, idemKey))
			resp, err := svc.CreateTransfer(ctxWithKey(idemKey), transferReq(tt.senderAccount, tt.receiverAccount, tt.amount))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.TransactionID != 1 {
				t.Errorf("TransactionID = %d, want 1", resp.TransactionID)
			}
			for _, p := range capturedUpdates {
				switch p.ID {
				case sender.ID:
					if *p.Balance != tt.wantSenderBal {
						t.Errorf("sender balance update = %d, want %d", *p.Balance, tt.wantSenderBal)
					}
				case receiver.ID:
					if *p.Balance != tt.wantReceiverBal {
						t.Errorf("receiver balance update = %d, want %d", *p.Balance, tt.wantReceiverBal)
					}
				}
			}
		})
	}
}

// ----------------------------------------------------------------------------
// TestCreateTransfer_Error
// ----------------------------------------------------------------------------

func TestCreateTransfer_Error(t *testing.T) {
	// setupTransactionMock wires a Transaction expectation that calls fn with mockDomain.
	setupTransactionMock := func(t *testing.T, r *txmocks.MockRepository, d *domainmock.MockDomainRepository) {
		t.Helper()
		r.EXPECT().Transaction(mock.Anything, mock.Anything).
			RunAndReturn(func(ctx context.Context, fn func(domain.DomainRepository) error) error {
				return fn(d)
			}).Once()
	}

	tests := []struct {
		name       string
		ctx        func() context.Context
		req        *model.CreateTransferRequest
		wantErr    error
		setupMocks func(t *testing.T, idemKey string) (*txmocks.MockRepository, *txmocks.MockCache)
	}{
		{
			name:    "missing idempotency key",
			ctx:     func() context.Context { return context.Background() },
			req:     transferReq("ACC-001", "ACC-002", 100),
			wantErr: ErrMissingIdempotencyKey,
			// Service returns before touching cache or repo.
			setupMocks: func(t *testing.T, _ string) (*txmocks.MockRepository, *txmocks.MockCache) {
				return txmocks.NewMockRepository(t), txmocks.NewMockCache(t)
			},
		},
		{
			name:    "sender account not found",
			ctx:     func() context.Context { return ctxWithKey(uuid.NewString()) },
			req:     transferReq("ACC-999", "ACC-002", 100),
			wantErr: ErrAccountNotFound,
			// nil check fires before GetTransferByReferenceID — no duplicate-check call expected.
			setupMocks: func(t *testing.T, idemKey string) (*txmocks.MockRepository, *txmocks.MockCache) {
				receiver := &dao.AccountBalances{ID: 2, AccountNumber: "ACC-002", Balance: 5_000_000}
				d := domainmock.NewMockDomainRepository(t)
				d.EXPECT().GetAccountsForUpdate(mock.Anything, mock.Anything).
					Return([]*dao.AccountBalances{receiver}, nil).Once()

				r := txmocks.NewMockRepository(t)
				setupTransactionMock(t, r, d)
				return r, newSuccessCacheMock(t, idemKey)
			},
		},
		{
			name:    "receiver account not found",
			ctx:     func() context.Context { return ctxWithKey(uuid.NewString()) },
			req:     transferReq("ACC-001", "ACC-999", 100),
			wantErr: ErrAccountNotFound,
			setupMocks: func(t *testing.T, idemKey string) (*txmocks.MockRepository, *txmocks.MockCache) {
				sender := &dao.AccountBalances{ID: 1, AccountNumber: "ACC-001", Balance: 10_000_000}
				d := domainmock.NewMockDomainRepository(t)
				d.EXPECT().GetAccountsForUpdate(mock.Anything, mock.Anything).
					Return([]*dao.AccountBalances{sender}, nil).Once()

				r := txmocks.NewMockRepository(t)
				setupTransactionMock(t, r, d)
				return r, newSuccessCacheMock(t, idemKey)
			},
		},
		{
			name:    "both accounts not found",
			ctx:     func() context.Context { return ctxWithKey(uuid.NewString()) },
			req:     transferReq("ACC-888", "ACC-999", 100),
			wantErr: ErrAccountNotFound,
			setupMocks: func(t *testing.T, idemKey string) (*txmocks.MockRepository, *txmocks.MockCache) {
				d := domainmock.NewMockDomainRepository(t)
				d.EXPECT().GetAccountsForUpdate(mock.Anything, mock.Anything).
					Return([]*dao.AccountBalances{}, nil).Once()

				r := txmocks.NewMockRepository(t)
				setupTransactionMock(t, r, d)
				return r, newSuccessCacheMock(t, idemKey)
			},
		},
		{
			name:    "insufficient balance",
			ctx:     func() context.Context { return ctxWithKey(uuid.NewString()) },
			req:     transferReq("ACC-003", "ACC-001", 100),
			wantErr: ErrInsufficientBalance,
			// Balance check fires before GetTransferByReferenceID — no duplicate-check call expected.
			setupMocks: func(t *testing.T, idemKey string) (*txmocks.MockRepository, *txmocks.MockCache) {
				sender := &dao.AccountBalances{ID: 3, AccountNumber: "ACC-003", Balance: 0}
				receiver := &dao.AccountBalances{ID: 1, AccountNumber: "ACC-001", Balance: 10_000_000}
				d := domainmock.NewMockDomainRepository(t)
				d.EXPECT().GetAccountsForUpdate(mock.Anything, mock.Anything).
					Return([]*dao.AccountBalances{sender, receiver}, nil).Once()

				r := txmocks.NewMockRepository(t)
				setupTransactionMock(t, r, d)
				return r, newSuccessCacheMock(t, idemKey)
			},
		},
		{
			name:    "amount exceeds balance by one",
			ctx:     func() context.Context { return ctxWithKey(uuid.NewString()) },
			req:     transferReq("ACC-002", "ACC-001", 5_000_001),
			wantErr: ErrInsufficientBalance,
			setupMocks: func(t *testing.T, idemKey string) (*txmocks.MockRepository, *txmocks.MockCache) {
				sender := &dao.AccountBalances{ID: 2, AccountNumber: "ACC-002", Balance: 5_000_000}
				receiver := &dao.AccountBalances{ID: 1, AccountNumber: "ACC-001", Balance: 10_000_000}
				d := domainmock.NewMockDomainRepository(t)
				d.EXPECT().GetAccountsForUpdate(mock.Anything, mock.Anything).
					Return([]*dao.AccountBalances{sender, receiver}, nil).Once()

				r := txmocks.NewMockRepository(t)
				setupTransactionMock(t, r, d)
				return r, newSuccessCacheMock(t, idemKey)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.ctx()
			idemKey, _ := ctx.Value(middleware.IdempotencyKey).(string)
			mockRepo, mockCache := tt.setupMocks(t, idemKey)

			svc := NewService(mockRepo, mockCache)
			_, err := svc.CreateTransfer(ctx, tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// TestCreateTransfer_IdempotentReplay
// ----------------------------------------------------------------------------

func TestCreateTransfer_IdempotentReplay(t *testing.T) {
	tests := []struct {
		name    string
		replays int
	}{
		{"single replay does not double-debit", 1},
		{"multiple replays all return same result", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idemKey := uuid.NewString()
			cacheKey := fmt.Sprintf(IdempotencyRequestKey, idemKey)
			sender := &dao.AccountBalances{ID: 1, AccountNumber: "ACC-001", Balance: 10_000_000}
			receiver := &dao.AccountBalances{ID: 2, AccountNumber: "ACC-002", Balance: 5_000_000}
			existingTransfer := &dao.TransactionTransfers{ID: 1, ReferenceID: idemKey}
			totalCalls := tt.replays + 1

			// First transaction: creates the transfer.
			firstDomain := domainmock.NewMockDomainRepository(t)
			firstDomain.EXPECT().GetAccountsForUpdate(mock.Anything, mock.Anything).
				Return([]*dao.AccountBalances{sender, receiver}, nil).Once()
			firstDomain.EXPECT().GetTransferByReferenceID(mock.Anything, idemKey).Return(nil, nil).Once()
			firstDomain.EXPECT().UpdateAccountBalance(mock.Anything, mock.Anything).Return(nil).Times(2)
			firstDomain.EXPECT().InsertTransfer(mock.Anything, mock.Anything).Return(int64(1), nil).Once()
			firstDomain.EXPECT().InsertLedgerEntriesBulk(mock.Anything, mock.Anything).Return(nil).Once()

			// Replay transactions: find the already-committed transfer and return early.
			replayDomain := domainmock.NewMockDomainRepository(t)
			replayDomain.EXPECT().GetAccountsForUpdate(mock.Anything, mock.Anything).
				Return([]*dao.AccountBalances{sender, receiver}, nil).Times(tt.replays)
			replayDomain.EXPECT().GetTransferByReferenceID(mock.Anything, idemKey).
				Return(existingTransfer, nil).Times(tt.replays)

			callCount := 0
			mockRepo := txmocks.NewMockRepository(t)
			mockRepo.EXPECT().Transaction(mock.Anything, mock.Anything).
				RunAndReturn(func(ctx context.Context, fn func(domain.DomainRepository) error) error {
					callCount++
					if callCount == 1 {
						return fn(firstDomain)
					}
					return fn(replayDomain)
				}).Times(totalCalls)

			mockCache := txmocks.NewMockCache(t)
			mockCache.EXPECT().SetNX(mock.Anything, cacheKey, idemKey, mock.Anything).
				Return(true, nil).Times(totalCalls)
			mockCache.EXPECT().Del(mock.Anything, cacheKey).Return(nil).Times(totalCalls)

			svc := NewService(mockRepo, mockCache)
			ctx := ctxWithKey(idemKey)
			req := transferReq("ACC-001", "ACC-002", 2_000_000)

			for i := range totalCalls {
				resp, err := svc.CreateTransfer(ctx, req)
				if err != nil {
					t.Fatalf("call %d failed: %v", i+1, err)
				}
				if resp.TransactionID != 1 {
					t.Errorf("call %d: TransactionID = %d, want 1", i+1, resp.TransactionID)
				}
			}
		})
	}
}
