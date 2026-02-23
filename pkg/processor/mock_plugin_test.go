// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package processor

import (
	"context"
	"sync"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage"
)

// MockPlugin is a generic test storage plugin that tracks writes.
type MockPlugin[T any] struct {
	name         string
	mu           sync.Mutex
	writeCalls   int
	totalEvents  int
	batches      [][]T
	filterFunc   func(T) bool
	shouldFail   bool
	failureError error
}

func NewMockPlugin[T any](name string) *MockPlugin[T] {
	return &MockPlugin[T]{
		name:       name,
		batches:    make([][]T, 0),
		filterFunc: func(T) bool { return true },
	}
}

func (m *MockPlugin[T]) Name() string { return m.name }

func (m *MockPlugin[T]) Initialize(_ context.Context) error { return nil }

func (m *MockPlugin[T]) Filter(event T) bool { return m.filterFunc(event) }

func (m *MockPlugin[T]) WriteBatch(_ context.Context, events []T) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.writeCalls++
	m.totalEvents += len(events)

	batch := make([]T, len(events))
	copy(batch, events)
	m.batches = append(m.batches, batch)

	if m.shouldFail {
		return 0, m.failureError
	}
	return len(events), nil
}

func (m *MockPlugin[T]) Close() error                        { return nil }
func (m *MockPlugin[T]) HealthCheck(_ context.Context) error { return nil }

// Compile-time check: MockPlugin[T] implements StoragePlugin[T].
var _ storage.StoragePlugin[any] = (*MockPlugin[any])(nil)

// Test helper methods

func (m *MockPlugin[T]) SetFilter(f func(T) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.filterFunc = f
}

func (m *MockPlugin[T]) SetShouldFail(shouldFail bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldFail = shouldFail
	m.failureError = err
}

func (m *MockPlugin[T]) GetWriteCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeCalls
}

func (m *MockPlugin[T]) GetTotalEvents() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.totalEvents
}

func (m *MockPlugin[T]) GetBatches() [][]T {
	m.mu.Lock()
	defer m.mu.Unlock()
	batches := make([][]T, len(m.batches))
	copy(batches, m.batches)
	return batches
}

func (m *MockPlugin[T]) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeCalls = 0
	m.totalEvents = 0
	m.batches = make([][]T, 0)
}
