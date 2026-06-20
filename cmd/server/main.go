package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/dhuki/go-ledger-system/internal/adapter/http/handler"
	httpV1 "github.com/dhuki/go-ledger-system/internal/adapter/http/v1"
	"github.com/dhuki/go-ledger-system/internal/core/healthcheck"
	"github.com/dhuki/go-ledger-system/internal/core/transaction"
	"github.com/dhuki/go-ledger-system/internal/infra/cache"
	"github.com/dhuki/go-ledger-system/internal/infra/configloader"
	"github.com/dhuki/go-ledger-system/internal/infra/database"
	"github.com/dhuki/go-ledger-system/internal/infra/logger"
	"github.com/dhuki/go-ledger-system/utils"
)

func init() {
	configloader.LoadConfig()
}

func main() {
	ctx := context.Background()

	psqlClient, err := database.NewPsqlClient(database.PostgresConfig{
		ApplicationName:         configloader.GetConfig("app_name").String(),
		Host:                    configloader.GetConfig("postgres_host").String(),
		Port:                    int(configloader.GetConfig("postgres_port").Int()),
		DBName:                  configloader.GetConfig("postgres_dbname").String(),
		Username:                configloader.GetConfig("postgres_username").String(),
		Password:                configloader.GetConfig("postgres_password").String(),
		SSLMode:                 configloader.GetConfig("postgres_ssl_mode").String(),
		MigrationDir:            configloader.GetConfig("postgres_migration_dir").String(),
		MaxConnection:           int(configloader.GetConfig("postgres_max_connection").Int()),
		MaxIdleConnection:       int(configloader.GetConfig("postgres_max_idle_connection").Int()),
		MaxDurationIdleConn:     configloader.GetConfig("postgres_max_duration_idle_conn").GetDuration(),
		MaxDurationLifetimeConn: configloader.GetConfig("postgres_max_duration_lifetime_conn").GetDuration(),
	})
	if err != nil {
		logger.Fatal(ctx, "Failed to connect to PostgreSQL", err)
	}

	domainRepo := psqlClient.NewPgRepository()

	cacheRepo, err := cache.NewRedisClient(ctx, cache.RedisConfig{
		Host:     configloader.GetConfig("redis_host").String(),
		Port:     int(configloader.GetConfig("redis_port").Int()),
		Password: configloader.GetConfig("redis_password").String(),
		DB:       int(configloader.GetConfig("redis_db").Int()),
	})
	if err != nil {
		logger.Fatal(ctx, "Failed to connect to Cache", err)
	}

	// #region health service
	healthSvc := healthcheck.NewService(domainRepo, cacheRepo)
	healthHandler := handler.NewHealthCheckHandler(healthSvc)
	// #endregion

	// #region transaction service
	transactionSvc := transaction.NewService(domainRepo, cacheRepo)
	transactionHandler := handler.NewTransactionHandler(transactionSvc)
	// #endregion

	httpRouter := httpV1.NewHTTPRouter(int(configloader.GetConfig("rest_port").Int()),
		transactionHandler, healthHandler,
	)

	wait := utils.GracefulShutdown(ctx, configloader.GetConfig("graceful_timeout").GetDuration(),
		httpRouter, psqlClient)

	logger.Info(ctx, "Success start service go-ledger-system, listening on port", configloader.GetConfig("rest_port").Int())
	go func() {
		if err := httpRouter.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(ctx, "httpRouter.Start", "Error starting service go-ledger-system, err : %v", err)
		}
	}()

	<-wait
}
