// Copyright (c) 2023-2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package bootstrap

import (
	"log/slog"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/config"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/dedup"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/events"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage"
)

// Deduplicators holds deduplicators for each event type
type Deduplicators struct {
	StatItemUpdated dedup.Deduplicator[*events.StatItemUpdatedEvent]
}

// InitializeDeduplicators creates deduplicators based on configuration
func InitializeDeduplicators(appCfg *config.Config, logger *slog.Logger) *Deduplicators {
	return &Deduplicators{
		StatItemUpdated: buildDeduplicator[*events.StatItemUpdatedEvent](appCfg, logger),
	}
}

// buildDeduplicator constructs a Deduplicator[T] based on the app configuration
func buildDeduplicator[T storage.Deduplicatable](appCfg *config.Config, logger *slog.Logger) dedup.Deduplicator[T] {
	if !appCfg.Deduplication.Enabled {
		logger.Info("deduplication disabled")
		return dedup.NewNoopDeduplicator[T]()
	}
	switch appCfg.Deduplication.Type {
	case "memory":
		logger.Info("deduplication enabled", "type", "memory", "ttl", appCfg.Deduplication.TTL)
		return dedup.NewMemoryDeduplicator[T](appCfg.Deduplication.TTL)
	case "redis":
		redisConfig := dedup.RedisConfig{
			Addr:     appCfg.Deduplication.Redis.Addr,
			Password: appCfg.Deduplication.Redis.Password,
			DB:       appCfg.Deduplication.Redis.DB,
		}
		logger.Info("deduplication enabled", "type", "redis", "addr", redisConfig.Addr, "ttl", appCfg.Deduplication.TTL)
		return dedup.NewRedisDeduplicator[T](redisConfig, appCfg.Deduplication.TTL)
	default:
		logger.Info("deduplication disabled")
		return dedup.NewNoopDeduplicator[T]()
	}
}

// CloseDeduplicators closes all deduplicators gracefully
func CloseDeduplicators(dedups *Deduplicators, logger *slog.Logger) {
	if err := dedups.StatItemUpdated.Close(); err != nil {
		logger.Error("stat_item_updated deduplicator close error", "error", err)
	}
}
