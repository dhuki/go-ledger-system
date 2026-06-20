package utils

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/dhuki/go-ledger-system/internal/infra/logger"
)

type Operation interface {
	Name() string
	Stop(ctx context.Context) error
}

func GracefulShutdown(ctx context.Context, timeout time.Duration, ops ...Operation) <-chan struct{} {
	wait := make(chan struct{})
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		<-sigint

		timeoutFunc := time.AfterFunc(timeout, func() {
			logger.Info(ctx, fmt.Sprintf("timeout %d ms has been elapsed, force exit app", timeout.Milliseconds()))
			os.Exit(0)
		})
		defer timeoutFunc.Stop()

		var wg sync.WaitGroup
		for _, op := range ops {
			wg.Go(func() {
				logger.Info(ctx, fmt.Sprintf("stopping operation: %s", op.Name()))
				if err := op.Stop(ctx); err != nil {
					logger.Error(ctx, fmt.Sprintf("operation failed: %v", err))
				}
			})
		}
		wg.Wait()
		close(wait)
	}()
	return wait
}
