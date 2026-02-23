// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package storage

// Deduplicatable defines the interface for events that support deduplication
// Any event type that wants to use the deduplicator must implement this interface
type Deduplicatable interface {
	// DeduplicationKey generates a unique identifier for the event
	// Used for deduplication and idempotency checks
	// Should combine namespace, user_id, session_id, timestamp, and other unique attributes
	DeduplicationKey() string
}
