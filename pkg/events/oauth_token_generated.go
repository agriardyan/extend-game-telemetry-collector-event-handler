// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package events

import "fmt"

// OauthTokenGeneratedEvent is the typed in-memory representation of an oauth token generated event.
// It carries server-enriched metadata alongside the decoded event fields.
type OauthTokenGeneratedEvent struct {
	ID              string
	Version         int64
	Namespace       string
	Name            string
	UserID          string
	SessionID       string
	TraceID         string
	Timestamp       string
	ServerTimestamp int64
	Payload         *OauthPayload
}

// OauthPayload holds the OAuth-specific fields from the event payload.
type OauthPayload struct {
	ClientID     string
	ResponseType string
	PlatformID   string
}

// DeduplicationKey returns a stable string used to identify duplicate events.
func (e *OauthTokenGeneratedEvent) DeduplicationKey() string {
	clientID := ""
	if e.Payload != nil {
		clientID = e.Payload.ClientID
	}
	return fmt.Sprintf("oauth_token_generated:%s:%s:%s:%s", e.Namespace, e.UserID, e.Timestamp, clientID)
}

// ToDocument returns the event as a flat map suitable for JSON/BSON serialization.
func (e *OauthTokenGeneratedEvent) ToDocument() map[string]interface{} {
	doc := map[string]interface{}{
		"kind":             "oauth_token_generated",
		"namespace":        e.Namespace,
		"user_id":          e.UserID,
		"server_timestamp": e.ServerTimestamp,
		"event_id":         e.ID,
		"event_name":       e.Name,
		"version":          e.Version,
		"timestamp":        e.Timestamp,
		"session_id":       e.SessionID,
		"trace_id":         e.TraceID,
	}
	if e.Payload != nil {
		doc["payload"] = map[string]interface{}{
			"client_id":     e.Payload.ClientID,
			"response_type": e.Payload.ResponseType,
			"platform_id":   e.Payload.PlatformID,
		}
	}
	return doc
}
