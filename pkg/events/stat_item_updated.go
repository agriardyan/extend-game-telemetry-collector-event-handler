// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package events

import (
	"fmt"

	statistic "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/pb/accelbyte-asyncapi/social/statistic/v1"
)

// StatItemUpdatedEvent is the typed in-memory representation of a stat item updated event.
// It carries server-enriched metadata alongside the raw protobuf payload.
type StatItemUpdatedEvent struct {
	Namespace       string
	UserID          string
	ServerTimestamp int64
	Payload         *statistic.StatItemUpdated
}

// DeduplicationKey returns a stable string used to identify duplicate events.
func (e *StatItemUpdatedEvent) DeduplicationKey() string {
	eventID := ""
	statCode := ""
	if e.Payload != nil {
		eventID = e.Payload.Id
		if e.Payload.Payload != nil {
			statCode = e.Payload.Payload.StatCode
		}
	}
	return fmt.Sprintf("stat_item_updated:%s:%s:%s:%s", e.Namespace, e.UserID, eventID, statCode)
}

// ToDocument returns the event as a flat map suitable for JSON/BSON serialization.
func (e *StatItemUpdatedEvent) ToDocument() map[string]interface{} {
	doc := map[string]interface{}{
		"kind":             "stat_item_updated",
		"namespace":        e.Namespace,
		"user_id":          e.UserID,
		"server_timestamp": e.ServerTimestamp,
	}
	if e.Payload != nil {
		doc["event_id"]         = e.Payload.Id
		doc["event_name"]       = e.Payload.Name
		doc["version"]          = e.Payload.Version
		doc["timestamp"]        = e.Payload.Timestamp
		doc["client_id"]        = e.Payload.ClientId
		doc["session_id"]       = e.Payload.SessionId
		doc["parent_namespace"] = e.Payload.ParentNamespace
		doc["payload"]          = e.Payload.Payload
	}
	return doc
}
