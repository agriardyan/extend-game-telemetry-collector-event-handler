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
	UserBehavior *processor.Processor[*events.UserBehaviorEvent]
	Gameplay     *processor.Processor[*events.GameplayEvent]
	Performance  *processor.Processor[*events.PerformanceEvent]
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
		UserBehavior: processor.NewProcessor(processorCfg, plugins.UserBehavior, dedups.UserBehavior, logger),
		Gameplay:     processor.NewProcessor(processorCfg, plugins.Gameplay, dedups.Gameplay, logger),
		Performance:  processor.NewProcessor(processorCfg, plugins.Performance, dedups.Performance, logger),
	}

	procs.UserBehavior.Start()
	procs.Gameplay.Start()
	procs.Performance.Start()

	logger.Info("async processors started",
		"workers", processorCfg.Workers,
		"channel_buffer", processorCfg.ChannelBuffer,
		"batch_size", processorCfg.BatchSize,
		"flush_interval", processorCfg.FlushInterval)

	return procs
}

// ShutdownProcessors gracefully shuts down all processors
func ShutdownProcessors(procs *Processors, timeout time.Duration, logger *slog.Logger) {
	if err := procs.UserBehavior.Shutdown(timeout); err != nil {
		logger.Error("user_behavior processor shutdown error", "error", err)
	}
	if err := procs.Gameplay.Shutdown(timeout); err != nil {
		logger.Error("gameplay processor shutdown error", "error", err)
	}
	if err := procs.Performance.Shutdown(timeout); err != nil {
		logger.Error("performance processor shutdown error", "error", err)
	}
}
