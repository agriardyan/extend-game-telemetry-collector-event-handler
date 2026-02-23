// Copyright (c) 2023-2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package bootstrap

import (
	"context"
	"log/slog"
	"strings"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/config"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/events"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage/plugins/kafka"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage/plugins/mongodb"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage/plugins/noop"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage/plugins/postgres"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage/plugins/s3"
)

// StoragePlugins holds all initialized storage plugins for each event type
type StoragePlugins struct {
	UserBehavior []storage.StoragePlugin[*events.UserBehaviorEvent]
	Gameplay     []storage.StoragePlugin[*events.GameplayEvent]
	Performance  []storage.StoragePlugin[*events.PerformanceEvent]
}

// InitializeStoragePlugins creates and initializes storage plugins based on configuration
func InitializeStoragePlugins(ctx context.Context, appCfg *config.Config, logger *slog.Logger) (*StoragePlugins, error) {
	plugins := &StoragePlugins{
		UserBehavior: []storage.StoragePlugin[*events.UserBehaviorEvent]{},
		Gameplay:     []storage.StoragePlugin[*events.GameplayEvent]{},
		Performance:  []storage.StoragePlugin[*events.PerformanceEvent]{},
	}

	for _, pluginName := range appCfg.GetEnabledPlugins() {
		var (
			ubPlugin   storage.StoragePlugin[*events.UserBehaviorEvent]
			gpPlugin   storage.StoragePlugin[*events.GameplayEvent]
			perfPlugin storage.StoragePlugin[*events.PerformanceEvent]
		)

		switch pluginName {
		case "postgres":
			ubPlugin = postgres.NewUserBehaviorPlugin(postgres.UserBehaviorPluginConfig{
				DSN:     appCfg.Storage.Postgres.PostgresDSN,
				Table:   "user_behavior_events",
				Workers: appCfg.Storage.Postgres.Workers,
			})
			gpPlugin = postgres.NewGameplayPlugin(postgres.GameplayPluginConfig{
				DSN:     appCfg.Storage.Postgres.PostgresDSN,
				Table:   "gameplay_events",
				Workers: appCfg.Storage.Postgres.Workers,
			})
			perfPlugin = postgres.NewPerformancePlugin(postgres.PerformancePluginConfig{
				DSN:     appCfg.Storage.Postgres.PostgresDSN,
				Table:   "performance_events",
				Workers: appCfg.Storage.Postgres.Workers,
			})

		case "s3":
			ubPlugin = s3.NewUserBehaviorPlugin(s3.UserBehaviorPluginConfig{
				Bucket:   appCfg.Storage.S3.S3Bucket,
				Prefix:   appCfg.Storage.S3.S3Prefix,
				Region:   appCfg.Storage.S3.S3Region,
				Endpoint: appCfg.Storage.S3.S3Endpoint,
			})
			gpPlugin = s3.NewGameplayPlugin(s3.GameplayPluginConfig{
				Bucket:   appCfg.Storage.S3.S3Bucket,
				Prefix:   appCfg.Storage.S3.S3Prefix,
				Region:   appCfg.Storage.S3.S3Region,
				Endpoint: appCfg.Storage.S3.S3Endpoint,
			})
			perfPlugin = s3.NewPerformancePlugin(s3.PerformancePluginConfig{
				Bucket:   appCfg.Storage.S3.S3Bucket,
				Prefix:   appCfg.Storage.S3.S3Prefix,
				Region:   appCfg.Storage.S3.S3Region,
				Endpoint: appCfg.Storage.S3.S3Endpoint,
			})

		case "kafka":
			brokers := strings.Split(appCfg.Storage.Kafka.KafkaBrokers, ",")
			for i, b := range brokers {
				brokers[i] = strings.TrimSpace(b)
			}
			baseTopic := appCfg.Storage.Kafka.KafkaTopic
			ubPlugin = kafka.NewUserBehaviorPlugin(kafka.UserBehaviorPluginConfig{
				Brokers:       brokers,
				Topic:         baseTopic + ".user_behavior",
				Compression:   appCfg.Storage.Kafka.KafkaCompression,
				BatchSize:     appCfg.Storage.Kafka.BatchSize,
				FlushInterval: appCfg.Storage.Kafka.FlushInterval,
			})
			gpPlugin = kafka.NewGameplayPlugin(kafka.GameplayPluginConfig{
				Brokers:       brokers,
				Topic:         baseTopic + ".gameplay",
				Compression:   appCfg.Storage.Kafka.KafkaCompression,
				BatchSize:     appCfg.Storage.Kafka.BatchSize,
				FlushInterval: appCfg.Storage.Kafka.FlushInterval,
			})
			perfPlugin = kafka.NewPerformancePlugin(kafka.PerformancePluginConfig{
				Brokers:       brokers,
				Topic:         baseTopic + ".performance",
				Compression:   appCfg.Storage.Kafka.KafkaCompression,
				BatchSize:     appCfg.Storage.Kafka.BatchSize,
				FlushInterval: appCfg.Storage.Kafka.FlushInterval,
			})

		case "mongodb":
			ubPlugin = mongodb.NewUserBehaviorPlugin(mongodb.UserBehaviorPluginConfig{
				URI:        appCfg.Storage.MongoDB.MongoURI,
				Database:   appCfg.Storage.MongoDB.MongoDatabase,
				Collection: "user_behavior_events",
				Workers:    appCfg.Storage.MongoDB.Workers,
			})
			gpPlugin = mongodb.NewGameplayPlugin(mongodb.GameplayPluginConfig{
				URI:        appCfg.Storage.MongoDB.MongoURI,
				Database:   appCfg.Storage.MongoDB.MongoDatabase,
				Collection: "gameplay_events",
				Workers:    appCfg.Storage.MongoDB.Workers,
			})
			perfPlugin = mongodb.NewPerformancePlugin(mongodb.PerformancePluginConfig{
				URI:        appCfg.Storage.MongoDB.MongoURI,
				Database:   appCfg.Storage.MongoDB.MongoDatabase,
				Collection: "performance_events",
				Workers:    appCfg.Storage.MongoDB.Workers,
			})

		case "noop":
			ubPlugin = noop.NewNoopPlugin[*events.UserBehaviorEvent]()
			gpPlugin = noop.NewNoopPlugin[*events.GameplayEvent]()
			perfPlugin = noop.NewNoopPlugin[*events.PerformanceEvent]()

		default:
			logger.Error("unknown plugin", "plugin", pluginName)
			continue
		}

		// Initialize all plugins
		if err := ubPlugin.Initialize(ctx); err != nil {
			return nil, err
		}
		logger.Info("plugin initialized", "plugin", ubPlugin.Name())

		if err := gpPlugin.Initialize(ctx); err != nil {
			return nil, err
		}
		logger.Info("plugin initialized", "plugin", gpPlugin.Name())

		if err := perfPlugin.Initialize(ctx); err != nil {
			return nil, err
		}
		logger.Info("plugin initialized", "plugin", perfPlugin.Name())

		plugins.UserBehavior = append(plugins.UserBehavior, ubPlugin)
		plugins.Gameplay = append(plugins.Gameplay, gpPlugin)
		plugins.Performance = append(plugins.Performance, perfPlugin)
	}

	if len(plugins.UserBehavior) == 0 {
		logger.Warn("no storage plugins enabled, using noop")
		noopUB := noop.NewNoopPlugin[*events.UserBehaviorEvent]()
		noopUB.Initialize(ctx)
		plugins.UserBehavior = append(plugins.UserBehavior, noopUB)

		noopGP := noop.NewNoopPlugin[*events.GameplayEvent]()
		noopGP.Initialize(ctx)
		plugins.Gameplay = append(plugins.Gameplay, noopGP)

		noopPerf := noop.NewNoopPlugin[*events.PerformanceEvent]()
		noopPerf.Initialize(ctx)
		plugins.Performance = append(plugins.Performance, noopPerf)
	}

	logger.Info("storage plugins ready",
		"user_behavior", len(plugins.UserBehavior),
		"gameplay", len(plugins.Gameplay),
		"performance", len(plugins.Performance))

	return plugins, nil
}

// CloseStoragePlugins closes all storage plugins gracefully
func CloseStoragePlugins(plugins *StoragePlugins, logger *slog.Logger) {
	for _, p := range plugins.UserBehavior {
		if err := p.Close(); err != nil {
			logger.Error("failed to close plugin", "plugin", p.Name(), "error", err)
		}
	}
	for _, p := range plugins.Gameplay {
		if err := p.Close(); err != nil {
			logger.Error("failed to close plugin", "plugin", p.Name(), "error", err)
		}
	}
	for _, p := range plugins.Performance {
		if err := p.Close(); err != nil {
			logger.Error("failed to close plugin", "plugin", p.Name(), "error", err)
		}
	}
}
