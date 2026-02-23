// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package events

import (
	"fmt"
)

// StatItemUpdatedEvent is the typed in-memory representation of a stat item updated event.
// It carries server-enriched metadata alongside the raw protobuf payload.
type StatItemUpdatedEvent struct {
	ID              string
	Version         int64
	Namespace       string
	Name            string
	UserID          string
	SessionID       string
	Timestamp       string
	ServerTimestamp int64
	Payload         *StatItem
}

type StatItem struct {
	StatCode                            string
	UserId                              string
	LatestValue                         float64
	Inc                                 float64
	AdditionalData                      map[string]interface{}
	IgnoreAdditionalDataOnValueRejected bool
	DefaultValue                        float64
	RequestValue                        float64
	UpdateStrategy                      string
}

// DeduplicationKey returns a stable string used to identify duplicate events.
func (e *StatItemUpdatedEvent) DeduplicationKey() string {
	eventTimestamp := e.Timestamp
	statCode := ""
	latestValue := 0.0
	if e.Payload != nil {
		statCode = e.Payload.StatCode
		latestValue = e.Payload.LatestValue
	}
	return fmt.Sprintf("stat_item_updated:%s:%s:%s:%s:%s", e.Namespace, e.UserID, eventTimestamp, latestValue, statCode)
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
		doc["event_id"] = e.ID
		doc["event_name"] = e.Name
		doc["version"] = e.Version
		doc["timestamp"] = e.Timestamp
		doc["session_id"] = e.SessionID
		doc["payload"] = map[string]interface{}{
			"stat_code":       e.Payload.StatCode,
			"user_id":         e.Payload.UserId,
			"latest_value":    e.Payload.LatestValue,
			"inc":             e.Payload.Inc,
			"additional_data": e.Payload.AdditionalData,
			"ignore_additional_data_on_value_rejected": e.Payload.IgnoreAdditionalDataOnValueRejected,
			"default_value":   e.Payload.DefaultValue,
			"request_value":   e.Payload.RequestValue,
			"update_strategy": e.Payload.UpdateStrategy,
		}
	}
	return doc
}
