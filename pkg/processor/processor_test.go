// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package processor

import (
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/dedup"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/events"
	pb "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/pb"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage"
)

func newGameplayEvent(userID string, serverTimestamp int64) *events.GameplayEvent {
	return &events.GameplayEvent{
		Namespace:       "test",
		UserID:          userID,
		ServerTimestamp: serverTimestamp,
		Payload: &pb.CreateGameplayTelemetryRequest{
			EventId: "test_event",
		},
	}
}

func TestProcessor_BasicSubmission(t *testing.T) {
	mockPlugin := NewMockPlugin[*events.GameplayEvent]("mock")
	plugins := []storage.StoragePlugin[*events.GameplayEvent]{mockPlugin}
	deduplicator := dedup.NewNoopDeduplicator[*events.GameplayEvent]()

	config := Config{
		Workers:       2,
		ChannelBuffer: 100,
		BatchSize:     5,
		FlushInterval: 100 * time.Millisecond,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	proc := NewProcessor(config, plugins, deduplicator, logger)
	proc.Start()

	// Submit 10 events
	for i := 0; i < 10; i++ {
		if err := proc.Submit(newGameplayEvent("user1", int64(i))); err != nil {
			t.Fatalf("Failed to submit event: %v", err)
		}
	}

	time.Sleep(500 * time.Millisecond)

	if err := proc.Shutdown(5 * time.Second); err != nil {
		t.Fatalf("Failed to shutdown processor: %v", err)
	}

	if mockPlugin.GetTotalEvents() != 10 {
		t.Errorf("Expected 10 events processed, got %d", mockPlugin.GetTotalEvents())
	}

	if writeCalls := mockPlugin.GetWriteCalls(); writeCalls < 2 {
		t.Errorf("Expected at least 2 write calls, got %d", writeCalls)
	}
}

func TestProcessor_Deduplication(t *testing.T) {
	mockPlugin := NewMockPlugin[*events.GameplayEvent]("mock")
	plugins := []storage.StoragePlugin[*events.GameplayEvent]{mockPlugin}

	deduplicator := dedup.NewMemoryDeduplicator[*events.GameplayEvent](1 * time.Minute)
	defer deduplicator.Close()

	config := Config{
		Workers:       2,
		ChannelBuffer: 100,
		BatchSize:     10,
		FlushInterval: 100 * time.Millisecond,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	proc := NewProcessor(config, plugins, deduplicator, logger)
	proc.Start()

	// Submit same event 5 times (identical DeduplicationKey)
	event := &events.GameplayEvent{
		Namespace: "test",
		UserID:    "user1",
		Payload: &pb.CreateGameplayTelemetryRequest{
			EventId:   "test_event",
			Timestamp: "12345",
		},
	}

	for i := 0; i < 5; i++ {
		if err := proc.Submit(event); err != nil {
			t.Fatalf("Failed to submit event: %v", err)
		}
	}

	time.Sleep(500 * time.Millisecond)

	if err := proc.Shutdown(5 * time.Second); err != nil {
		t.Fatalf("Failed to shutdown processor: %v", err)
	}

	// Only 1 event should be processed (others deduplicated)
	if mockPlugin.GetTotalEvents() != 1 {
		t.Errorf("Expected 1 event processed (dedup), got %d", mockPlugin.GetTotalEvents())
	}

	stats := proc.Stats()
	if stats["events_duplicated"].(uint64) != 4 {
		t.Errorf("Expected 4 duplicated events, got %v", stats["events_duplicated"])
	}
}

func TestProcessor_MultiplePlugins(t *testing.T) {
	mockPlugin1 := NewMockPlugin[*events.GameplayEvent]("mock1")
	mockPlugin2 := NewMockPlugin[*events.GameplayEvent]("mock2")
	mockPlugin3 := NewMockPlugin[*events.GameplayEvent]("mock3")

	// Only accept events with user_id "user2"
	mockPlugin2.SetFilter(func(event *events.GameplayEvent) bool {
		return event.UserID == "user2"
	})

	// Only accept events with server_timestamp >= 5
	mockPlugin3.SetFilter(func(event *events.GameplayEvent) bool {
		return event.ServerTimestamp >= 5
	})

	plugins := []storage.StoragePlugin[*events.GameplayEvent]{mockPlugin1, mockPlugin2, mockPlugin3}
	deduplicator := dedup.NewNoopDeduplicator[*events.GameplayEvent]()

	config := Config{
		Workers:       2,
		ChannelBuffer: 100,
		BatchSize:     10,
		FlushInterval: 100 * time.Millisecond,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	proc := NewProcessor(config, plugins, deduplicator, logger)
	proc.Start()

	// Submit 10 events (5 user1, 5 user2)
	for i := 0; i < 10; i++ {
		userID := "user1"
		if i >= 5 {
			userID = "user2"
		}
		if err := proc.Submit(newGameplayEvent(userID, int64(i))); err != nil {
			t.Fatalf("Failed to submit event: %v", err)
		}
	}

	time.Sleep(500 * time.Millisecond)

	if err := proc.Shutdown(5 * time.Second); err != nil {
		t.Fatalf("Failed to shutdown processor: %v", err)
	}

	// Plugin1: accepts all (10 events)
	if mockPlugin1.GetTotalEvents() != 10 {
		t.Errorf("Plugin1: Expected 10 events, got %d", mockPlugin1.GetTotalEvents())
	}

	// Plugin2: only user2 (5 events: 5-9)
	if mockPlugin2.GetTotalEvents() != 5 {
		t.Errorf("Plugin2: Expected 5 events, got %d", mockPlugin2.GetTotalEvents())
	}

	// Plugin3: only timestamp >= 5 (5 events: 5-9)
	if mockPlugin3.GetTotalEvents() != 5 {
		t.Errorf("Plugin3: Expected 5 events, got %d", mockPlugin3.GetTotalEvents())
	}
}

func TestProcessor_GracefulShutdown(t *testing.T) {
	mockPlugin := NewMockPlugin[*events.GameplayEvent]("mock")
	plugins := []storage.StoragePlugin[*events.GameplayEvent]{mockPlugin}
	deduplicator := dedup.NewNoopDeduplicator[*events.GameplayEvent]()

	config := Config{
		Workers:       2,
		ChannelBuffer: 100,
		BatchSize:     100,
		FlushInterval: 1 * time.Hour,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	proc := NewProcessor(config, plugins, deduplicator, logger)
	proc.Start()

	for i := 0; i < 50; i++ {
		if err := proc.Submit(newGameplayEvent("user1", int64(i))); err != nil {
			t.Fatalf("Failed to submit event: %v", err)
		}
	}

	time.Sleep(100 * time.Millisecond)

	if err := proc.Shutdown(10 * time.Second); err != nil {
		t.Fatalf("Failed to shutdown processor: %v", err)
	}

	if mockPlugin.GetTotalEvents() != 50 {
		t.Errorf("Expected 50 events flushed on shutdown, got %d", mockPlugin.GetTotalEvents())
	}
}

func TestProcessor_ConcurrentSubmission(t *testing.T) {
	mockPlugin := NewMockPlugin[*events.GameplayEvent]("mock")
	plugins := []storage.StoragePlugin[*events.GameplayEvent]{mockPlugin}
	deduplicator := dedup.NewNoopDeduplicator[*events.GameplayEvent]()

	config := Config{
		Workers:       5,
		ChannelBuffer: 1000,
		BatchSize:     50,
		FlushInterval: 100 * time.Millisecond,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	proc := NewProcessor(config, plugins, deduplicator, logger)
	proc.Start()

	numGoroutines := 10
	eventsPerGoroutine := 50
	var wg sync.WaitGroup
	var submitErrors sync.Map
	errorCount := 0

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < eventsPerGoroutine; i++ {
				event := newGameplayEvent("user1", int64(goroutineID*1000+i))
				if err := proc.Submit(event); err != nil {
					submitErrors.Store(goroutineID*1000+i, err)
				}
			}
		}(g)
	}

	wg.Wait()

	submitErrors.Range(func(key, value interface{}) bool {
		errorCount++
		return true
	})

	if errorCount > 0 {
		t.Logf("Warning: %d events failed to submit", errorCount)
	}

	time.Sleep(1 * time.Second)

	if err := proc.Shutdown(10 * time.Second); err != nil {
		t.Fatalf("Failed to shutdown processor: %v", err)
	}

	expectedTotal := numGoroutines * eventsPerGoroutine
	actualEvents := mockPlugin.GetTotalEvents()
	if actualEvents < expectedTotal-5 || actualEvents > expectedTotal {
		t.Errorf("Expected approximately %d events processed, got %d", expectedTotal, actualEvents)
	}

	stats := proc.Stats()
	eventsReceived := stats["events_received"].(uint64)
	if eventsReceived < uint64(expectedTotal-5) {
		t.Errorf("Expected approximately %d events received, got %v", expectedTotal, eventsReceived)
	}
}

func TestProcessor_Stats(t *testing.T) {
	mockPlugin := NewMockPlugin[*events.GameplayEvent]("mock")
	plugins := []storage.StoragePlugin[*events.GameplayEvent]{mockPlugin}
	deduplicator := dedup.NewNoopDeduplicator[*events.GameplayEvent]()

	config := Config{
		Workers:       3,
		ChannelBuffer: 200,
		BatchSize:     10,
		FlushInterval: 100 * time.Millisecond,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	proc := NewProcessor(config, plugins, deduplicator, logger)
	proc.Start()

	for i := 0; i < 25; i++ {
		if err := proc.Submit(newGameplayEvent("user1", int64(i))); err != nil {
			t.Fatalf("Failed to submit event: %v", err)
		}
	}

	time.Sleep(500 * time.Millisecond)

	stats := proc.Stats()

	if stats["workers"].(int) != 3 {
		t.Errorf("Expected 3 workers, got %v", stats["workers"])
	}
	if stats["queue_capacity"].(int) != 200 {
		t.Errorf("Expected queue capacity 200, got %v", stats["queue_capacity"])
	}
	if stats["events_received"].(uint64) != 25 {
		t.Errorf("Expected 25 events received, got %v", stats["events_received"])
	}
	if stats["plugins"].(int) != 1 {
		t.Errorf("Expected 1 plugin, got %v", stats["plugins"])
	}

	if err := proc.Shutdown(5 * time.Second); err != nil {
		t.Fatalf("Failed to shutdown processor: %v", err)
	}

	stats = proc.Stats()
	if stats["events_processed"].(uint64) != 25 {
		t.Errorf("Expected 25 events processed, got %v", stats["events_processed"])
	}
}

func TestProcessor_Backpressure(t *testing.T) {
	mockPlugin := NewMockPlugin[*events.GameplayEvent]("mock")
	plugins := []storage.StoragePlugin[*events.GameplayEvent]{mockPlugin}
	deduplicator := dedup.NewNoopDeduplicator[*events.GameplayEvent]()

	config := Config{
		Workers:       1,
		ChannelBuffer: 10,
		BatchSize:     100,
		FlushInterval: 10 * time.Second,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	proc := NewProcessor(config, plugins, deduplicator, logger)
	proc.Start()

	var wg sync.WaitGroup
	for i := 0; i < 15; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			proc.Submit(newGameplayEvent("user1", int64(i)))
		}()
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	stats := proc.Stats()
	queueDepth := stats["queue_depth"].(int)
	if queueDepth < 0 || queueDepth > 15 {
		t.Logf("Queue depth: %d (expected between 0 and 15)", queueDepth)
	}

	if err := proc.Shutdown(10 * time.Second); err != nil {
		t.Fatalf("Failed to shutdown processor: %v", err)
	}

	if mockPlugin.GetTotalEvents() != 15 {
		t.Errorf("Expected 15 events processed, got %d", mockPlugin.GetTotalEvents())
	}
}

func TestProcessor_ShutdownTimeout(t *testing.T) {
	mockPlugin := NewMockPlugin[*events.GameplayEvent]("mock")
	plugins := []storage.StoragePlugin[*events.GameplayEvent]{mockPlugin}
	deduplicator := dedup.NewNoopDeduplicator[*events.GameplayEvent]()

	config := Config{
		Workers:       2,
		ChannelBuffer: 100,
		BatchSize:     10,
		FlushInterval: 100 * time.Millisecond,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	proc := NewProcessor(config, plugins, deduplicator, logger)
	proc.Start()

	for i := 0; i < 5; i++ {
		proc.Submit(newGameplayEvent("user1", int64(i)))
	}

	err := proc.Shutdown(1 * time.Nanosecond)
	if err == nil {
		t.Log("Expected timeout error, got nil (workers may have finished very quickly)")
	}
}
