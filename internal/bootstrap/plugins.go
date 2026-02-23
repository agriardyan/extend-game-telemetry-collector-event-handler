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
	StatItemUpdated    []storage.StoragePlugin[*events.StatItemUpdatedEvent]
	OauthTokenGenerated []storage.StoragePlugin[*events.OauthTokenGeneratedEvent]
}

// InitializeStoragePlugins creates and initializes storage plugins based on configuration
func InitializeStoragePlugins(ctx context.Context, appCfg *config.Config, logger *slog.Logger) (*StoragePlugins, error) {
	plugins := &StoragePlugins{
		StatItemUpdated:    []storage.StoragePlugin[*events.StatItemUpdatedEvent]{},
		OauthTokenGenerated: []storage.StoragePlugin[*events.OauthTokenGeneratedEvent]{},
	}

	for _, pluginName := range appCfg.GetEnabledPlugins() {
		var (
			siuPlugin storage.StoragePlugin[*events.StatItemUpdatedEvent]
			otgPlugin storage.StoragePlugin[*events.OauthTokenGeneratedEvent]
		)

		switch pluginName {
		case "postgres":
			siuPlugin = postgres.NewStatItemUpdatedPlugin(postgres.StatItemUpdatedPluginConfig{
				DSN:     appCfg.Storage.Postgres.PostgresDSN,
				Table:   "stat_item_updated_events",
				Workers: appCfg.Storage.Postgres.Workers,
			})
			otgPlugin = postgres.NewOauthTokenGeneratedPlugin(postgres.OauthTokenGeneratedPluginConfig{
				DSN:     appCfg.Storage.Postgres.PostgresDSN,
				Table:   "oauth_token_generated_events",
				Workers: appCfg.Storage.Postgres.Workers,
			})

		case "s3":
			siuPlugin = s3.NewStatItemUpdatedPlugin(s3.StatItemUpdatedPluginConfig{
				Bucket:   appCfg.Storage.S3.S3Bucket,
				Prefix:   appCfg.Storage.S3.S3Prefix,
				Region:   appCfg.Storage.S3.S3Region,
				Endpoint: appCfg.Storage.S3.S3Endpoint,
			})
			otgPlugin = s3.NewOauthTokenGeneratedPlugin(s3.OauthTokenGeneratedPluginConfig{
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
			siuPlugin = kafka.NewStatItemUpdatedPlugin(kafka.StatItemUpdatedPluginConfig{
				Brokers:       brokers,
				Topic:         baseTopic + ".stat_item_updated",
				Compression:   appCfg.Storage.Kafka.KafkaCompression,
				BatchSize:     appCfg.Storage.Kafka.BatchSize,
				FlushInterval: appCfg.Storage.Kafka.FlushInterval,
			})
			otgPlugin = kafka.NewOauthTokenGeneratedPlugin(kafka.OauthTokenGeneratedPluginConfig{
				Brokers:       brokers,
				Topic:         baseTopic + ".oauth_token_generated",
				Compression:   appCfg.Storage.Kafka.KafkaCompression,
				BatchSize:     appCfg.Storage.Kafka.BatchSize,
				FlushInterval: appCfg.Storage.Kafka.FlushInterval,
			})

		case "mongodb":
			siuPlugin = mongodb.NewStatItemUpdatedPlugin(mongodb.StatItemUpdatedPluginConfig{
				URI:        appCfg.Storage.MongoDB.MongoURI,
				Database:   appCfg.Storage.MongoDB.MongoDatabase,
				Collection: "stat_item_updated_events",
				Workers:    appCfg.Storage.MongoDB.Workers,
			})
			otgPlugin = mongodb.NewOauthTokenGeneratedPlugin(mongodb.OauthTokenGeneratedPluginConfig{
				URI:        appCfg.Storage.MongoDB.MongoURI,
				Database:   appCfg.Storage.MongoDB.MongoDatabase,
				Collection: "oauth_token_generated_events",
				Workers:    appCfg.Storage.MongoDB.Workers,
			})

		case "noop":
			siuPlugin = noop.NewNoopPlugin[*events.StatItemUpdatedEvent]()
			otgPlugin = noop.NewNoopPlugin[*events.OauthTokenGeneratedEvent]()

		default:
			logger.Error("unknown plugin", "plugin", pluginName)
			continue
		}

		// Initialize all plugins

		if err := siuPlugin.Initialize(ctx); err != nil {
			return nil, err
		}
		logger.Info("plugin initialized", "plugin", siuPlugin.Name())
		plugins.StatItemUpdated = append(plugins.StatItemUpdated, siuPlugin)

		if err := otgPlugin.Initialize(ctx); err != nil {
			return nil, err
		}
		logger.Info("plugin initialized", "plugin", otgPlugin.Name())
		plugins.OauthTokenGenerated = append(plugins.OauthTokenGenerated, otgPlugin)
	}

	if len(plugins.StatItemUpdated) == 0 {
		logger.Warn("no storage plugins enabled, using noop")

		noopSIU := noop.NewNoopPlugin[*events.StatItemUpdatedEvent]()
		noopSIU.Initialize(ctx)
		plugins.StatItemUpdated = append(plugins.StatItemUpdated, noopSIU)
	}

	if len(plugins.OauthTokenGenerated) == 0 {
		noopOTG := noop.NewNoopPlugin[*events.OauthTokenGeneratedEvent]()
		noopOTG.Initialize(ctx)
		plugins.OauthTokenGenerated = append(plugins.OauthTokenGenerated, noopOTG)
	}

	logger.Info("storage plugins ready",
		"stat_item_updated", len(plugins.StatItemUpdated),
		"oauth_token_generated", len(plugins.OauthTokenGenerated))

	return plugins, nil
}

// CloseStoragePlugins closes all storage plugins gracefully
func CloseStoragePlugins(plugins *StoragePlugins, logger *slog.Logger) {
	for _, p := range plugins.StatItemUpdated {
		if err := p.Close(); err != nil {
			logger.Error("failed to close plugin", "plugin", p.Name(), "error", err)
		}
	}
	for _, p := range plugins.OauthTokenGenerated {
		if err := p.Close(); err != nil {
			logger.Error("failed to close plugin", "plugin", p.Name(), "error", err)
		}
	}
}
