// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package processor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/dedup"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage"
)

// Processor handles async processing of telemetry events
// It receives events via a buffered channel, batches them, and distributes to storage plugins
// Generic implementation supports any event type
type Processor[T storage.Deduplicatable] struct {
	// Configuration
	workers       int
	batchSize     int
	flushInterval time.Duration

	// Components
	inputChan    chan T
	plugins      []storage.StoragePlugin[T]
	deduplicator dedup.Deduplicator[T]
	logger       *slog.Logger

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	eventsReceived   atomic.Uint64
	eventsProcessed  atomic.Uint64
	eventsDuplicated atomic.Uint64
	eventsDropped    atomic.Uint64
}

// Config holds processor configuration
type Config struct {
	Workers       int
	ChannelBuffer int
	BatchSize     int
	FlushInterval time.Duration
}

// NewProcessor creates a new async telemetry processor
func NewProcessor[T storage.Deduplicatable](
	config Config,
	plugins []storage.StoragePlugin[T],
	deduplicator dedup.Deduplicator[T],
	logger *slog.Logger,
) *Processor[T] {
	ctx, cancel := context.WithCancel(context.Background())

	return &Processor[T]{
		workers:       config.Workers,
		batchSize:     config.BatchSize,
		flushInterval: config.FlushInterval,
		inputChan:     make(chan T, config.ChannelBuffer),
		plugins:       plugins,
		deduplicator:  deduplicator,
		logger:        logger.With("component", "processor"),
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start launches the worker pool
func (p *Processor[T]) Start() {
	p.logger.Info("starting processor",
		"workers", p.workers,
		"channel_buffer", cap(p.inputChan),
		"batch_size", p.batchSize,
		"flush_interval", p.flushInterval,
		"plugins", len(p.plugins))

	// Start worker goroutines
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}

	p.logger.Info("processor started")
}

// worker processes events from the input channel
func (p *Processor[T]) worker(id int) {
	defer p.wg.Done()

	logger := p.logger.With("worker_id", id)
	logger.Info("worker started")

	// Create a batcher for this worker
	batcher := NewBatcher(
		p.batchSize,
		p.flushInterval,
		func(batch []T) {
			p.processBatch(logger, batch)
		},
	)

	for {
		select {
		case event, ok := <-p.inputChan:
			if !ok {
				// Channel closed - flush remaining events and exit
				logger.Info("input channel closed, flushing remaining events")
				batcher.Flush()
				logger.Info("worker shutting down")
				return
			}

			// Add event to batch
			batcher.Add(event)

		case <-p.ctx.Done():
			// Context cancelled - flush and exit
			logger.Info("context cancelled, flushing remaining events")
			batcher.Flush()
			logger.Info("worker shutting down")
			return
		}
	}
}

// processBatch distributes a batch of events to all storage plugins in parallel
func (p *Processor[T]) processBatch(logger *slog.Logger, batch []T) {
	if len(batch) == 0 {
		return
	}

	logger.Debug("processing batch", "size", len(batch))

	// Fan out to all plugins in parallel
	var wg sync.WaitGroup

	for _, plugin := range p.plugins {
		wg.Add(1)
		go func(plugin storage.StoragePlugin[T]) {
			defer wg.Done()
			p.writeToPlugin(logger, plugin, batch)
		}(plugin)
	}

	wg.Wait()

	// Update metrics
	p.eventsProcessed.Add(uint64(len(batch)))

	logger.Debug("batch processing complete", "size", len(batch))
}

// writeToPlugin writes a batch of events to a single storage plugin
func (p *Processor[T]) writeToPlugin(logger *slog.Logger, plugin storage.StoragePlugin[T], batch []T) {
	// Filter events for this plugin
	filtered := make([]T, 0, len(batch))
	for _, event := range batch {
		if plugin.Filter(event) {
			filtered = append(filtered, event)
		}
	}

	if len(filtered) == 0 {
		logger.Debug("no events passed filter",
			"plugin", plugin.Name(),
			"total", len(batch))
		return
	}

	// Write batch with timeout
	ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
	defer cancel()

	start := time.Now()
	count, err := plugin.WriteBatch(ctx, filtered)
	duration := time.Since(start)

	if err != nil {
		logger.Error("failed to write batch",
			"plugin", plugin.Name(),
			"error", err,
			"attempted", len(filtered),
			"succeeded", count,
			"duration", duration)
		return
	}

	logger.Info("batch written successfully",
		"plugin", plugin.Name(),
		"count", count,
		"duration", duration)
}

// Submit queues an event for async processing
// Returns immediately after queuing (non-blocking for caller)
func (p *Processor[T]) Submit(event T) error {
	// Check for duplicates if deduplicator is configured
	if p.deduplicator != nil {
		ctx, cancel := context.WithTimeout(p.ctx, 100*time.Millisecond)
		defer cancel()

		isDupe, err := p.deduplicator.IsDuplicate(ctx, event)
		if err != nil {
			p.logger.Warn("deduplication check failed, processing anyway",
				"error", err,
				"event_key", event.DeduplicationKey())
		} else if isDupe {
			p.logger.Debug("duplicate event detected, skipping",
				"event_key", event.DeduplicationKey())

			p.eventsDuplicated.Add(1)
			return nil // Skip duplicate
		}

		// Mark as seen
		if err := p.deduplicator.Mark(ctx, event); err != nil {
			p.logger.Warn("failed to mark event as seen",
				"error", err)
			// Continue processing even if mark fails
		}
	}

	// Try to submit to channel
	select {
	case p.inputChan <- event:
		p.eventsReceived.Add(1)
		return nil

	default:
		// Channel full - apply backpressure
		p.logger.Warn("input channel full, applying backpressure",
			"queue_depth", len(p.inputChan),
			"capacity", cap(p.inputChan))

		// Block until space available (adds latency but preserves data)
		select {
		case p.inputChan <- event:
			p.eventsReceived.Add(1)
			return nil

		case <-p.ctx.Done():
			p.eventsDropped.Add(1)
			return fmt.Errorf("processor is shutting down")
		}
	}
}

// Shutdown gracefully stops the processor
// Waits for all workers to finish processing pending events
func (p *Processor[T]) Shutdown(timeout time.Duration) error {
	p.logger.Info("shutting down processor", "timeout", timeout)

	// Stop accepting new events
	close(p.inputChan)

	// Wait for workers to finish or timeout
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Info("all workers shut down gracefully",
			"events_received", p.eventsReceived.Load(),
			"events_processed", p.eventsProcessed.Load(),
			"events_duplicated", p.eventsDuplicated.Load(),
			"events_dropped", p.eventsDropped.Load())
		return nil

	case <-time.After(timeout):
		p.logger.Warn("shutdown timeout exceeded, forcing exit",
			"timeout", timeout,
			"events_received", p.eventsReceived.Load(),
			"events_processed", p.eventsProcessed.Load(),
			"events_duplicated", p.eventsDuplicated.Load(),
			"events_dropped", p.eventsDropped.Load())

		// Cancel context to force workers to exit
		p.cancel()

		return fmt.Errorf("shutdown timeout exceeded")
	}
}

// Stats returns current processor statistics
func (p *Processor[T]) Stats() map[string]interface{} {
	return map[string]interface{}{
		"workers":           p.workers,
		"queue_depth":       len(p.inputChan),
		"queue_capacity":    cap(p.inputChan),
		"events_received":   p.eventsReceived.Load(),
		"events_processed":  p.eventsProcessed.Load(),
		"events_duplicated": p.eventsDuplicated.Load(),
		"events_dropped":    p.eventsDropped.Load(),
		"plugins":           len(p.plugins),
	}
}
