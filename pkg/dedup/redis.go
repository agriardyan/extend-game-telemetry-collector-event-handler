// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package dedup

import (
	"context"
	"log/slog"
	"time"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage"
	"github.com/redis/go-redis/v9"
)

// RedisDeduplicator uses Redis to track seen events
// Suitable for distributed deployments where multiple instances need shared deduplication
// Generic implementation supports any event type that implements storage.Deduplicatable
type RedisDeduplicator[T storage.Deduplicatable] struct {
	client *redis.Client
	ttl    time.Duration
	prefix string
	logger *slog.Logger
}

// RedisConfig holds Redis connection configuration
type RedisConfig struct {
	Addr     string // Redis server address (host:port)
	Password string // Redis password (optional)
	DB       int    // Redis database number (0-15)
}

// NewRedisDeduplicator creates a Redis-based deduplicator
func NewRedisDeduplicator[T storage.Deduplicatable](config RedisConfig, ttl time.Duration) *RedisDeduplicator[T] {
	client := redis.NewClient(&redis.Options{
		Addr:     config.Addr,
		Password: config.Password,
		DB:       config.DB,
	})

	d := &RedisDeduplicator[T]{
		client: client,
		ttl:    ttl,
		prefix: "telemetry:dedup:",
		logger: slog.Default().With("component", "redis_deduplicator"),
	}

	d.logger.Info("redis deduplicator initialized",
		"addr", config.Addr,
		"ttl", ttl)

	return d
}

// IsDuplicate checks if the event fingerprint exists in Redis
func (d *RedisDeduplicator[T]) IsDuplicate(ctx context.Context, event T) (bool, error) {
	key := d.redisKey(event)

	exists, err := d.client.Exists(ctx, key).Result()
	if err != nil {
		d.logger.Error("failed to check duplicate",
			"error", err,
			"key", key)
		return false, err
	}

	return exists > 0, nil
}

// Mark adds the event fingerprint to Redis with TTL
func (d *RedisDeduplicator[T]) Mark(ctx context.Context, event T) error {
	key := d.redisKey(event)

	// Set key with TTL (value doesn't matter, we just check existence)
	err := d.client.Set(ctx, key, "1", d.ttl).Err()
	if err != nil {
		d.logger.Error("failed to mark event",
			"error", err,
			"key", key)
		return err
	}

	return nil
}

// redisKey generates the Redis key for an event
func (d *RedisDeduplicator[T]) redisKey(event T) string {
	return d.prefix + EventFingerprint(event.DeduplicationKey())
}

// Close closes the Redis connection
func (d *RedisDeduplicator[T]) Close() error {
	d.logger.Info("closing redis deduplicator")
	return d.client.Close()
}

// Ping checks if Redis is accessible
func (d *RedisDeduplicator[T]) Ping(ctx context.Context) error {
	return d.client.Ping(ctx).Err()
}

// Stats returns Redis connection statistics
func (d *RedisDeduplicator[T]) Stats(ctx context.Context) (map[string]interface{}, error) {
	info, err := d.client.Info(ctx, "stats").Result()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"connected": true,
		"ttl":       d.ttl.String(),
		"info":      info,
	}, nil
}
