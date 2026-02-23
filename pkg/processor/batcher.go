// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package processor

import (
	"sync"
	"time"
)

// Batcher accumulates events and flushes them based on size or time triggers
// This enables efficient bulk writes to storage backends
// Generic implementation supports any event type
type Batcher[T any] struct {
	events    []T
	mu        sync.Mutex
	maxSize   int
	maxWait   time.Duration
	timer     *time.Timer
	flushFunc func([]T)
}

// NewBatcher creates a new batcher with size and time-based flushing
func NewBatcher[T any](maxSize int, maxWait time.Duration, flushFunc func([]T)) *Batcher[T] {
	return &Batcher[T]{
		events:    make([]T, 0, maxSize),
		maxSize:   maxSize,
		maxWait:   maxWait,
		flushFunc: flushFunc,
	}
}

// Add appends an event to the batch
// Flushes immediately if batch size is reached
func (b *Batcher[T]) Add(event T) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Add to batch
	b.events = append(b.events, event)

	// Start timer on first event
	if len(b.events) == 1 {
		b.timer = time.AfterFunc(b.maxWait, b.timeoutFlush)
	}

	// Flush if batch size reached
	if len(b.events) >= b.maxSize {
		b.flush()
	}
}

// flush sends the current batch to the flush function
// Must be called with mutex locked
func (b *Batcher[T]) flush() {
	if len(b.events) == 0 {
		return
	}

	// Stop timer if active
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}

	// Copy events to avoid data races
	batch := make([]T, len(b.events))
	copy(batch, b.events)

	// Reset batch
	b.events = b.events[:0]

	// Call flush function directly (synchronous)
	// This ensures flushes complete before returning (important for graceful shutdown)
	b.flushFunc(batch)
}

// timeoutFlush is called when the max wait time is exceeded
func (b *Batcher[T]) timeoutFlush() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flush()
}

// Flush manually flushes any pending events
// This is called during graceful shutdown
func (b *Batcher[T]) Flush() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flush()
}

// Size returns the current number of events in the batch
func (b *Batcher[T]) Size() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events)
}
