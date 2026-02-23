// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package events

import (
	"fmt"

	pb "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/pb"
)

// GameplayEvent is the typed representation of a gameplay telemetry event.
// It carries server-enriched metadata alongside the raw protobuf payload.
type GameplayEvent struct {
	Namespace       string
	UserID          string
	ServerTimestamp int64
	SourceIP        string
	Payload         *pb.CreateGameplayTelemetryRequest
}

// DeduplicationKey returns a stable string used for deduplication.
func (e *GameplayEvent) DeduplicationKey() string {
	eventID := ""
	timestamp := ""
	if e.Payload != nil {
		eventID = e.Payload.EventId
		timestamp = e.Payload.Timestamp
	}
	return fmt.Sprintf("gameplay:%s:%s:%s:%s", e.Namespace, e.UserID, eventID, timestamp)
}

// ToDocument returns the event as a flat map suitable for JSON/BSON serialization.
func (e *GameplayEvent) ToDocument() map[string]interface{} {
	doc := map[string]interface{}{
		"kind":             "gameplay",
		"namespace":        e.Namespace,
		"user_id":          e.UserID,
		"server_timestamp": e.ServerTimestamp,
		"source_ip":        e.SourceIP,
	}
	if e.Payload != nil {
		doc["event_id"] = e.Payload.EventId
		doc["version"] = e.Payload.Version
		doc["timestamp"] = e.Payload.Timestamp
		doc["payload"] = e.Payload
	}
	return doc
}
