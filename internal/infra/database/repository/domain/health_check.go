package domain

import "context"

type HealthCheck interface {
	Ping(ctx context.Context) error
}

func (r *domainRepository) Ping(ctx context.Context) error {
	return r.dbImpl.PingContext(ctx)
}
