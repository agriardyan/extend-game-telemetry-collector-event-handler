// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package dedup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage"
)

// Deduplicator determines if telemetry events are duplicates
// Implementations can use in-memory caches, Redis, or other storage mechanisms
// Generic implementation supports any event type that implements storage.Deduplicatable
type Deduplicator[T storage.Deduplicatable] interface {
	// IsDuplicate checks if the event has been seen before
	// Returns true if the event is a duplicate, false if it's new
	IsDuplicate(ctx context.Context, event T) (bool, error)

	// Mark registers an event as seen to prevent future duplicates
	Mark(ctx context.Context, event T) error

	// Close cleans up resources used by the deduplicator
	Close() error
}

// EventFingerprint generates a unique fingerprint using SHA256 hash
// This is a helper function for deduplicator implementations
func EventFingerprint(key string) string {
	h := sha256.New()
	h.Write([]byte(key))
	return hex.EncodeToString(h.Sum(nil))
}

// NoopDeduplicator is a deduplicator that never detects duplicates
// Useful when deduplication is disabled
type NoopDeduplicator[T storage.Deduplicatable] struct{}

// NewNoopDeduplicator creates a no-op deduplicator
func NewNoopDeduplicator[T storage.Deduplicatable]() Deduplicator[T] {
	return &NoopDeduplicator[T]{}
}

// IsDuplicate always returns false (never a duplicate)
func (n *NoopDeduplicator[T]) IsDuplicate(ctx context.Context, event T) (bool, error) {
	return false, nil
}

// Mark does nothing
func (n *NoopDeduplicator[T]) Mark(ctx context.Context, event T) error {
	return nil
}

// Close does nothing
func (n *NoopDeduplicator[T]) Close() error {
	return nil
}
