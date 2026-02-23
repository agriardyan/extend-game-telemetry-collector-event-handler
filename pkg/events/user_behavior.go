// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package events

import (
	"fmt"

	pb "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/pb"
)

// UserBehaviorEvent is the typed representation of a user behavior telemetry event.
// It carries server-enriched metadata alongside the raw protobuf payload.
type UserBehaviorEvent struct {
	Namespace       string
	UserID          string
	ServerTimestamp int64
	SourceIP        string
	Payload         *pb.CreateUserBehaviorTelemetryRequest
}

// DeduplicationKey returns a stable string used for deduplication.
func (e *UserBehaviorEvent) DeduplicationKey() string {
	sessionID := ""
	if e.Payload != nil && e.Payload.Context != nil {
		sessionID = e.Payload.Context.SessionId
	}
	eventID := ""
	if e.Payload != nil {
		eventID = e.Payload.EventId
	}
	return fmt.Sprintf("user_behavior:%s:%s:%s:%s", e.Namespace, e.UserID, eventID, sessionID)
}

// ToDocument returns the event as a flat map suitable for JSON/BSON serialization.
func (e *UserBehaviorEvent) ToDocument() map[string]interface{} {
	doc := map[string]interface{}{
		"kind":             "user_behavior",
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
