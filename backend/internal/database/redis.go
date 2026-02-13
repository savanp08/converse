package database

import (
	"context"
	"fmt"
	"log"

	"github.com/redis/go-redis/v9"
)

var Ctx = context.Background()

type RedisStore struct {
	Client *redis.Client
}

func InitRedis(addr, password string) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	})

	if _, err := rdb.Ping(Ctx).Result(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	log.Println("✅ Connected to Redis")
	return rdb
}

func NewRedisStore(addr, password string) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	})

	if _, err := client.Ping(Ctx).Result(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &RedisStore{Client: client}, nil
}

func (r *RedisStore) Publish(ctx context.Context, channel string, payload []byte) error {
	return r.Client.Publish(ctx, channel, payload).Err()
}

func (r *RedisStore) Close() error {
	return r.Client.Close()
}
