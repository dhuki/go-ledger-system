package healthcheck

import "context"

type Service interface {
	HealthCheck(ctx context.Context) (err error)
}

type Repository interface {
	Ping(ctx context.Context) (err error)
}

type Cache interface {
	Ping(ctx context.Context) error
}
