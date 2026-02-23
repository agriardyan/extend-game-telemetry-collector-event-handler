// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package storage

import (
	"context"
	"time"
)

// StoragePlugin defines the contract for all storage backend implementations.
// Each plugin is responsible for filtering, writing, health checking, and
// graceful shutdown. Configuration is injected via the plugin's constructor —
// Initialize only performs connection setup and resource allocation.
// Generic implementation supports any event type.
type StoragePlugin[T any] interface {
	// Name returns the unique identifier for this plugin (e.g., "s3", "postgres", "kafka")
	Name() string

	// Initialize sets up connections and resources using the config already
	// supplied to the constructor. Called once during application startup.
	Initialize(ctx context.Context) error

	// Filter determines if an event should be stored by this plugin.
	// Return true to accept the event, false to skip it.
	// ------------------------------------------------------------------------------
	// DEVELOPER NOTE:
	// This method is where you can implement custom filtering logic to control which events are processed by this plugin.
	// For example, you might want to filter out certain event types, namespaces, or events from specific users.
	// You can modify this logic as needed for your specific use case.
	// ------------------------------------------------------------------------------
	Filter(event T) bool

	// WriteBatch writes a batch of events to the storage backend.
	// Returns the number of successfully written events and any error.
	// Implementations should handle partial failures gracefully.
	// ------------------------------------------------------------------------------
	// DEVELOPER NOTE:
	// This method is where you implement the core logic to write events to your storage backend.
	// You should handle data transformation, batching, retries, and error handling here.
	// The current implementation assumes a simple batch write, but you can customize it as needed.
	// ------------------------------------------------------------------------------
	WriteBatch(ctx context.Context, events []T) (int, error)

	// Close gracefully shuts down the plugin, flushing any pending data
	// and releasing resources (connections, file handles, etc.)
	Close() error

	// HealthCheck verifies that the storage backend is accessible and operational.
	// Returns nil if healthy, error otherwise.
	HealthCheck(ctx context.Context) error
}

// RetryPolicy defines how failed operations should be retried
type RetryPolicy struct {
	// MaxRetries is the maximum number of retry attempts (0 = no retries)
	MaxRetries int

	// InitialBackoff is the delay before the first retry
	InitialBackoff time.Duration

	// MaxBackoff is the maximum delay between retries
	MaxBackoff time.Duration

	// Multiplier is the backoff multiplier for exponential backoff
	// Each retry delay = previous delay * Multiplier (capped at MaxBackoff)
	Multiplier float64
}

// DefaultRetryPolicy returns a sensible default retry configuration
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxRetries:     3,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     10 * time.Second,
		Multiplier:     2.0,
	}
}

// NextBackoff calculates the next retry delay using exponential backoff
func (rp *RetryPolicy) NextBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return rp.InitialBackoff
	}

	// Calculate exponential backoff
	backoff := rp.InitialBackoff
	for i := 0; i < attempt; i++ {
		backoff = time.Duration(float64(backoff) * rp.Multiplier)
		if backoff > rp.MaxBackoff {
			return rp.MaxBackoff
		}
	}

	return backoff
}
