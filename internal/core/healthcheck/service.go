package healthcheck

import "context"

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

func (s *service) HealthCheck(ctx context.Context) (err error) {
	if err := s.repo.Ping(ctx); err != nil {
		return err
	}

	if err := s.cache.Ping(ctx); err != nil {
		return err
	}

	return nil
}
