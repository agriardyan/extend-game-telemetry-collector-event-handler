// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package noop

import (
	"context"
	"log/slog"
)

// NoopPlugin is a storage plugin that accepts all events but doesn't store them
// Useful for testing, development, and as a reference implementation
// Generic implementation supports any event type
type NoopPlugin[T any] struct {
	logger *slog.Logger
}

// NewNoopPlugin creates a new noop plugin instance
func NewNoopPlugin[T any]() *NoopPlugin[T] {
	return &NoopPlugin[T]{}
}

// Name returns the plugin identifier
func (p *NoopPlugin[T]) Name() string {
	return "noop"
}

// Initialize sets up the no-op plugin
func (p *NoopPlugin[T]) Initialize(ctx context.Context) error {
	p.logger = slog.Default().With("plugin", "noop")
	p.logger.Info("noop plugin initialized (events will be discarded)")
	return nil
}

// Filter determines if an event should be processed by this plugin
// ------------------------------------------------------------------------------
// DEVELOPER NOTE:
// This method is where you can implement custom filtering logic to control which events are processed by this plugin.
// For example, you might want to filter out certain event types, namespaces, or events from specific users.
// You can modify this logic as needed for your specific use case.
// ------------------------------------------------------------------------------
func (p *NoopPlugin[T]) Filter(event T) bool {
	return true
}

// transform converts an event into a format suitable for this plugin's insertion
// ------------------------------------------------------------------------------
// DEVELOPER NOTE:
// This method is where you can customize how the event data is transformed before being stored.
// For example, you might want to flatten nested properties, convert timestamps, or filter out certain fields.
// You can modify this logic as needed for your specific use case.
// ------------------------------------------------------------------------------
func (p *NoopPlugin[T]) transform(event T) (interface{}, error) {
	return event, nil
}

// WriteBatch pretends to write events but actually discards them
func (p *NoopPlugin[T]) WriteBatch(ctx context.Context, events []T) (int, error) {
	p.logger.Debug("noop write batch", "count", len(events))

	// Simulate successful write of all events
	return len(events), nil
}

// Close cleans up resources (none for no-op)
func (p *NoopPlugin[T]) Close() error {
	p.logger.Info("noop plugin closed")
	return nil
}

// HealthCheck always returns healthy
func (p *NoopPlugin[T]) HealthCheck(ctx context.Context) error {
	return nil
}
