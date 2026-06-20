package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	queryRepo "github.com/dhuki/go-ledger-system/internal/infra/database/repository"
	"github.com/dhuki/go-ledger-system/internal/infra/database/repository/domain"
	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
)

type PsqlClient interface {
	Name() string
	Stop(ctx context.Context) error
	NewPgRepository() domain.DomainRepository
}

type PostgresConfig struct {
	Host                    string
	Port                    int
	DBName                  string
	Username                string
	Password                string
	SSLMode                 string
	ApplicationName         string
	MigrationDir            string
	MaxConnection           int
	MaxIdleConnection       int
	MaxDurationIdleConn     time.Duration
	MaxDurationLifetimeConn time.Duration
}

type PsqlConn struct {
	conn *sql.DB
}

func NewPsqlClient(conf PostgresConfig) (PsqlClient, error) {
	if conf.Host == "" {
		return nil, fmt.Errorf("postgres host cannot be empty")
	}
	if conf.Port == 0 {
		return nil, fmt.Errorf("postgres port cannot be empty")
	}

	sslMode := conf.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}

	appName := conf.ApplicationName
	if appName == "" {
		appName = "go-ledger-system"
	}

	dsn := fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s application_name=%s",
		conf.Host, conf.Port, conf.DBName, conf.Username, conf.Password, sslMode, appName,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("Failed to open PostgreSQL connection: %w", err)
	}

	if conf.MaxConnection > 0 {
		db.SetMaxOpenConns(conf.MaxConnection)
	}
	if conf.MaxIdleConnection > 0 {
		db.SetMaxIdleConns(conf.MaxIdleConnection)
	}
	if conf.MaxDurationIdleConn > 0 {
		db.SetConnMaxIdleTime(conf.MaxDurationIdleConn)
	}
	if conf.MaxDurationLifetimeConn > 0 {
		db.SetConnMaxLifetime(conf.MaxDurationLifetimeConn)
	}

	if err := db.PingContext(context.Background()); err != nil {
		return nil, fmt.Errorf("Failed to ping PostgreSQL: %w", err)
	}

	if err := migrate(db, conf.MigrationDir); err != nil {
		return nil, err
	}

	return &PsqlConn{conn: db}, nil
}

func migrate(db *sql.DB, migrationDir string) error {
	if migrationDir == "" {
		return nil
	}
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("Failed to set goose dialect: %w", err)
	}
	if err := goose.Up(db, migrationDir); err != nil {
		return fmt.Errorf("Failed to run migrations: %w", err)
	}
	return nil
}

func (p *PsqlConn) Name() string {
	return "PostgreSQL Client"
}

func (p *PsqlConn) Stop(ctx context.Context) error {
	return p.conn.Close()
}

func (p *PsqlConn) NewPgRepository() domain.DomainRepository {
	queryRepo := queryRepo.NewQueryRepository(p.conn)
	domainRepo := domain.NewRepositoryDomain(queryRepo)
	return domainRepo
}
