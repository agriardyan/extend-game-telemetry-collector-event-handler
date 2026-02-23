// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package processor

import (
	"sync"
	"testing"
	"time"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/events"
	pb "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/pb"
)

func makeGameplayEvent(serverTimestamp int64) *events.GameplayEvent {
	return &events.GameplayEvent{
		Namespace:       "test",
		UserID:          "user1",
		ServerTimestamp: serverTimestamp,
		Payload: &pb.CreateGameplayTelemetryRequest{
			EventId: "test_event",
		},
	}
}

func TestBatcher_SizeBasedFlush(t *testing.T) {
	maxSize := 5
	maxWait := 1 * time.Hour

	var mu sync.Mutex
	var flushedBatches [][]*events.GameplayEvent

	flushFunc := func(batch []*events.GameplayEvent) {
		mu.Lock()
		defer mu.Unlock()
		flushedBatches = append(flushedBatches, batch)
	}

	batcher := NewBatcher(maxSize, maxWait, flushFunc)

	for i := 0; i < 12; i++ {
		batcher.Add(makeGameplayEvent(int64(i)))
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(flushedBatches) != 2 {
		t.Errorf("Expected 2 flushed batches, got %d", len(flushedBatches))
	}

	for i, batch := range flushedBatches {
		if len(batch) != maxSize {
			t.Errorf("Batch %d: expected %d events, got %d", i, maxSize, len(batch))
		}
	}

	if batcher.Size() != 2 {
		t.Errorf("Expected 2 events remaining in batcher, got %d", batcher.Size())
	}
}

func TestBatcher_TimeBasedFlush(t *testing.T) {
	maxSize := 100
	maxWait := 200 * time.Millisecond

	var mu sync.Mutex
	var flushedBatches [][]*events.GameplayEvent

	flushFunc := func(batch []*events.GameplayEvent) {
		mu.Lock()
		defer mu.Unlock()
		flushedBatches = append(flushedBatches, batch)
	}

	batcher := NewBatcher(maxSize, maxWait, flushFunc)

	for i := 0; i < 3; i++ {
		batcher.Add(makeGameplayEvent(int64(i)))
	}

	time.Sleep(maxWait + 100*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(flushedBatches) != 1 {
		t.Errorf("Expected 1 flushed batch, got %d", len(flushedBatches))
	}
	if len(flushedBatches) > 0 && len(flushedBatches[0]) != 3 {
		t.Errorf("Expected 3 events in batch, got %d", len(flushedBatches[0]))
	}
	if batcher.Size() != 0 {
		t.Errorf("Expected 0 events remaining in batcher, got %d", batcher.Size())
	}
}

func TestBatcher_ManualFlush(t *testing.T) {
	maxSize := 100
	maxWait := 1 * time.Hour

	var mu sync.Mutex
	var flushedBatches [][]*events.GameplayEvent

	flushFunc := func(batch []*events.GameplayEvent) {
		mu.Lock()
		defer mu.Unlock()
		flushedBatches = append(flushedBatches, batch)
	}

	batcher := NewBatcher(maxSize, maxWait, flushFunc)

	for i := 0; i < 5; i++ {
		batcher.Add(makeGameplayEvent(int64(i)))
	}

	batcher.Flush()
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(flushedBatches) != 1 {
		t.Errorf("Expected 1 flushed batch, got %d", len(flushedBatches))
	}
	if len(flushedBatches) > 0 && len(flushedBatches[0]) != 5 {
		t.Errorf("Expected 5 events in batch, got %d", len(flushedBatches[0]))
	}
	if batcher.Size() != 0 {
		t.Errorf("Expected 0 events remaining in batcher, got %d", batcher.Size())
	}
}

func TestBatcher_EmptyFlush(t *testing.T) {
	maxSize := 100
	maxWait := 1 * time.Hour

	var mu sync.Mutex
	var flushedBatches [][]*events.GameplayEvent

	flushFunc := func(batch []*events.GameplayEvent) {
		mu.Lock()
		defer mu.Unlock()
		flushedBatches = append(flushedBatches, batch)
	}

	batcher := NewBatcher(maxSize, maxWait, flushFunc)
	batcher.Flush()
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(flushedBatches) != 0 {
		t.Errorf("Expected 0 flushed batches, got %d", len(flushedBatches))
	}
}

func TestBatcher_ConcurrentAdds(t *testing.T) {
	maxSize := 50
	maxWait := 100 * time.Millisecond

	var mu sync.Mutex
	var flushedBatches [][]*events.GameplayEvent
	totalFlushed := 0

	flushFunc := func(batch []*events.GameplayEvent) {
		mu.Lock()
		defer mu.Unlock()
		flushedBatches = append(flushedBatches, batch)
		totalFlushed += len(batch)
	}

	batcher := NewBatcher(maxSize, maxWait, flushFunc)

	numGoroutines := 10
	eventsPerGoroutine := 20
	var wg sync.WaitGroup

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < eventsPerGoroutine; i++ {
				batcher.Add(makeGameplayEvent(int64(goroutineID*1000 + i)))
			}
		}(g)
	}

	wg.Wait()
	batcher.Flush()
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	expectedTotal := numGoroutines * eventsPerGoroutine
	if totalFlushed != expectedTotal {
		t.Errorf("Expected %d total flushed events, got %d", expectedTotal, totalFlushed)
	}

	// Verify no duplicate events (unique by ServerTimestamp)
	eventSet := make(map[int64]bool)
	for _, batch := range flushedBatches {
		for _, event := range batch {
			if eventSet[event.ServerTimestamp] {
				t.Errorf("Duplicate event with server_timestamp %d", event.ServerTimestamp)
			}
			eventSet[event.ServerTimestamp] = true
		}
	}
}

func TestBatcher_TimerCancellation(t *testing.T) {
	maxSize := 5
	maxWait := 200 * time.Millisecond

	var mu sync.Mutex
	var flushedBatches [][]*events.GameplayEvent

	flushFunc := func(batch []*events.GameplayEvent) {
		mu.Lock()
		defer mu.Unlock()
		flushedBatches = append(flushedBatches, batch)
	}

	batcher := NewBatcher(maxSize, maxWait, flushFunc)

	// Add 3 events (starts timer)
	for i := 0; i < 3; i++ {
		batcher.Add(makeGameplayEvent(int64(i)))
	}

	// Add 2 more to trigger size-based flush (should cancel timer)
	time.Sleep(50 * time.Millisecond)
	for i := 3; i < 5; i++ {
		batcher.Add(makeGameplayEvent(int64(i)))
	}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(flushedBatches) != 1 {
		t.Errorf("Expected 1 flushed batch, got %d", len(flushedBatches))
	}

	// Wait to ensure timer doesn't fire
	time.Sleep(maxWait + 100*time.Millisecond)

	if len(flushedBatches) != 1 {
		t.Errorf("Expected 1 flushed batch after timer wait, got %d", len(flushedBatches))
	}
}
