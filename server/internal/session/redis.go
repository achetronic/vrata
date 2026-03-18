package session

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore implements Store using Redis as the backend.
type RedisStore struct {
	client *redis.Client
}

// NewRedisStore creates a RedisStore connected to the given address.
func NewRedisStore(addr, password string, db int) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, err
	}
	return &RedisStore{client: client}, nil
}

func redisKey(sid, routeID string) string {
	return "vrata:sticky:" + sid + ":" + routeID
}

// Get returns the destination ID for the given session+route pair.
func (s *RedisStore) Get(ctx context.Context, sid, routeID string) (string, error) {
	val, err := s.client.Get(ctx, redisKey(sid, routeID)).Result()
	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

// Set stores the mapping from session+route to destination with a TTL.
func (s *RedisStore) Set(ctx context.Context, sid, routeID, destID string, ttlSeconds int) error {
	ttl := time.Duration(ttlSeconds) * time.Second
	return s.client.Set(ctx, redisKey(sid, routeID), destID, ttl).Err()
}

// Close releases the Redis connection.
func (s *RedisStore) Close() error {
	return s.client.Close()
}
