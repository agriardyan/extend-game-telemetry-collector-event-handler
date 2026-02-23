// Copyright (c) 2023-2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package bootstrap

import (
	"log/slog"
	"time"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/config"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/events"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/processor"
)

// Processors holds processors for each event type
type Processors struct {
	StatItemUpdated *processor.Processor[*events.StatItemUpdatedEvent]
}

// InitializeProcessors creates and starts processors for each event type
func InitializeProcessors(
	appCfg *config.Config,
	plugins *StoragePlugins,
	dedups *Deduplicators,
	logger *slog.Logger,
) *Processors {
	processorCfg := processor.Config{
		Workers:       appCfg.Processor.Workers,
		ChannelBuffer: appCfg.Processor.ChannelBuffer,
		BatchSize:     appCfg.Processor.DefaultBatchSize,
		FlushInterval: appCfg.Processor.DefaultFlushInterval,
	}

	procs := &Processors{
		StatItemUpdated: processor.NewProcessor(processorCfg, plugins.StatItemUpdated, dedups.StatItemUpdated, logger),
	}

	procs.StatItemUpdated.Start()

	logger.Info("async processors started",
		"workers", processorCfg.Workers,
		"channel_buffer", processorCfg.ChannelBuffer,
		"batch_size", processorCfg.BatchSize,
		"flush_interval", processorCfg.FlushInterval)

	return procs
}

// ShutdownProcessors gracefully shuts down all processors
func ShutdownProcessors(procs *Processors, timeout time.Duration, logger *slog.Logger) {
	if err := procs.StatItemUpdated.Shutdown(timeout); err != nil {
		logger.Error("stat_item_updated processor shutdown error", "error", err)
	}
}
