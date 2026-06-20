package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisClient interface {
	Name() string
	Stop(ctx context.Context) error
	Ping(ctx context.Context) error
	SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error)
	Del(ctx context.Context, key string) error
}

type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
}

type RedisConn struct {
	cli *redis.Client
}

func NewRedisClient(ctx context.Context, conf RedisConfig) (RedisClient, error) {
	redisClient := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", conf.Host, conf.Port),
		Password: conf.Password,
		DB:       conf.DB,
	})

	if _, err := redisClient.Ping(ctx).Result(); err != nil {
		return nil, err
	}
	return &RedisConn{
		cli: redisClient,
	}, nil
}

func (r *RedisConn) Name() string {
	return "Redis Client"
}

func (r *RedisConn) Stop(ctx context.Context) error {
	return r.cli.Close()
}

func (r *RedisConn) Ping(ctx context.Context) error {
	return r.cli.Ping(ctx).Err()
}

func (r *RedisConn) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	return r.cli.SetNX(ctx, key, value, ttl).Result()
}

func (r *RedisConn) Del(ctx context.Context, key string) error {
	return r.cli.Del(ctx, key).Err()
}
