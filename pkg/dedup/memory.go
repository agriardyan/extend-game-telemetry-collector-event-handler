// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package dedup

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage"
)

// MemoryDeduplicator uses an in-memory map to track seen events
// Suitable for single-instance deployments or when exact deduplication isn't critical
// Generic implementation supports any event type that implements storage.Deduplicatable
type MemoryDeduplicator[T storage.Deduplicatable] struct {
	cache       map[string]time.Time
	ttl         time.Duration
	mu          sync.RWMutex
	stopCleanup chan struct{}
	logger      *slog.Logger
}

// NewMemoryDeduplicator creates an in-memory deduplicator with TTL-based cleanup
func NewMemoryDeduplicator[T storage.Deduplicatable](ttl time.Duration) *MemoryDeduplicator[T] {
	d := &MemoryDeduplicator[T]{
		cache:       make(map[string]time.Time),
		ttl:         ttl,
		stopCleanup: make(chan struct{}),
		logger:      slog.Default().With("component", "memory_deduplicator"),
	}

	// Start background cleanup goroutine
	go d.cleanup()

	d.logger.Info("memory deduplicator initialized", "ttl", ttl)

	return d
}

// IsDuplicate checks if the event fingerprint exists in the cache
func (d *MemoryDeduplicator[T]) IsDuplicate(ctx context.Context, event T) (bool, error) {
	key := EventFingerprint(event.DeduplicationKey())

	d.mu.RLock()
	_, exists := d.cache[key]
	d.mu.RUnlock()

	return exists, nil
}

// Mark adds the event fingerprint to the cache with current timestamp
func (d *MemoryDeduplicator[T]) Mark(ctx context.Context, event T) error {
	key := EventFingerprint(event.DeduplicationKey())

	d.mu.Lock()
	d.cache[key] = time.Now()
	d.mu.Unlock()

	return nil
}

// cleanup periodically removes expired entries from the cache
func (d *MemoryDeduplicator[T]) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.removeExpired()

		case <-d.stopCleanup:
			d.logger.Info("stopping cleanup goroutine")
			return
		}
	}
}

// removeExpired removes entries older than TTL from the cache
func (d *MemoryDeduplicator[T]) removeExpired() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	removed := 0

	for key, timestamp := range d.cache {
		if now.Sub(timestamp) > d.ttl {
			delete(d.cache, key)
			removed++
		}
	}

	if removed > 0 {
		d.logger.Debug("cleaned up expired entries",
			"removed", removed,
			"remaining", len(d.cache))
	}
}

// Close stops the cleanup goroutine and clears the cache
func (d *MemoryDeduplicator[T]) Close() error {
	close(d.stopCleanup)

	d.mu.Lock()
	d.cache = make(map[string]time.Time)
	d.mu.Unlock()

	d.logger.Info("memory deduplicator closed")
	return nil
}

// Stats returns current cache statistics
func (d *MemoryDeduplicator[T]) Stats() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return map[string]interface{}{
		"entries": len(d.cache),
		"ttl":     d.ttl.String(),
	}
}
