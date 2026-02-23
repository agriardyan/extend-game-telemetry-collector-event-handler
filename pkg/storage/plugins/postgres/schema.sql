-- Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
-- This is licensed software from AccelByte Inc, for limitations
-- and restrictions contact your company contract manager.

-- Telemetry Events Table
-- Stores game telemetry data with JSONB for flexible property storage

CREATE TABLE IF NOT EXISTS telemetry_events (
    id BIGSERIAL PRIMARY KEY,

    -- Event identification
    namespace VARCHAR(255) NOT NULL,
    event_name VARCHAR(255) NOT NULL,

    -- User and session info
    user_id VARCHAR(255),
    session_id VARCHAR(255),

    -- Timestamps (Unix milliseconds)
    timestamp BIGINT NOT NULL,           -- Client timestamp
    server_timestamp BIGINT NOT NULL,    -- Server receive timestamp

    -- Flexible properties storage
    properties JSONB,

    -- Metadata
    source_ip VARCHAR(45),               -- IPv4 or IPv6
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for common query patterns

-- 1. Query by namespace and time range (most common)
CREATE INDEX IF NOT EXISTS idx_telemetry_events_namespace_timestamp
    ON telemetry_events(namespace, timestamp DESC);

-- 2. Query by user (player analytics)
CREATE INDEX IF NOT EXISTS idx_telemetry_events_user_id
    ON telemetry_events(user_id)
    WHERE user_id IS NOT NULL;

-- 3. Query by event type (event-specific analytics)
CREATE INDEX IF NOT EXISTS idx_telemetry_events_event_name
    ON telemetry_events(event_name);

-- 4. Query by session (session replay)
CREATE INDEX IF NOT EXISTS idx_telemetry_events_session_id
    ON telemetry_events(session_id)
    WHERE session_id IS NOT NULL;

-- 5. JSONB properties search (flexible querying)
CREATE INDEX IF NOT EXISTS idx_telemetry_events_properties
    ON telemetry_events USING GIN(properties);

-- 6. Server timestamp for data pipeline ordering
CREATE INDEX IF NOT EXISTS idx_telemetry_events_server_timestamp
    ON telemetry_events(server_timestamp DESC);

-- Example queries:

-- Get events for a specific user in the last 24 hours
-- SELECT * FROM telemetry_events
-- WHERE user_id = 'user123'
-- AND timestamp > (EXTRACT(EPOCH FROM NOW() - INTERVAL '24 hours') * 1000)
-- ORDER BY timestamp DESC;

-- Get events with specific property
-- SELECT * FROM telemetry_events
-- WHERE properties @> '{"level": "5"}';

-- Count events by type
-- SELECT event_name, COUNT(*)
-- FROM telemetry_events
-- WHERE namespace = 'my-game'
-- GROUP BY event_name;
