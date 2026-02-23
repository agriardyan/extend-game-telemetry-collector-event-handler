// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/events"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage"

	_ "github.com/lib/pq"
)

// UserBehaviorPluginConfig holds Postgres configuration for the user behavior plugin.
// Each telemetry type plugin owns its config independently, allowing different
// DSNs, tables, or pool sizes per event category.
type UserBehaviorPluginConfig struct {
	DSN     string // required, e.g. "postgres://user:pass@host/db?sslmode=disable"
	Table   string // default "user_behavior_events"
	Workers int    // connection pool size; default 2
}

// UserBehaviorPlugin stores user behavior telemetry events in a PostgreSQL table.
// It manages its own database connection and is fully independent of sibling Postgres plugins.
type UserBehaviorPlugin struct {
	cfg    UserBehaviorPluginConfig
	db     *sql.DB
	logger *slog.Logger
}

// NewUserBehaviorPlugin creates a Postgres plugin for user behavior events.
func NewUserBehaviorPlugin(cfg UserBehaviorPluginConfig) storage.StoragePlugin[*events.UserBehaviorEvent] {
	if cfg.Table == "" {
		cfg.Table = "user_behavior_events"
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 2
	}
	return &UserBehaviorPlugin{cfg: cfg}
}

func (p *UserBehaviorPlugin) Name() string { return "postgres:user_behavior" }

func (p *UserBehaviorPlugin) Initialize(ctx context.Context) error {
	p.logger = slog.Default().With("plugin", p.Name())

	if p.cfg.DSN == "" {
		return fmt.Errorf("postgres DSN is required")
	}

	db, err := sql.Open("postgres", p.cfg.DSN)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	maxOpenConns := p.cfg.Workers * 2
	if maxOpenConns < 10 {
		maxOpenConns = 10
	}
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxOpenConns / 2)

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	p.db = db

	if err := p.createTableIfNotExists(ctx); err != nil {
		return fmt.Errorf("failed to create table %s: %w", p.cfg.Table, err)
	}

	p.logger.Info("postgres plugin initialized", "table", p.cfg.Table, "max_open_conns", maxOpenConns)
	return nil
}

// createTableIfNotExists ensures the target table and indexes exist before writing data.
// ----------------------------------------------------------------------------
// DEVELOPER NOTE:
// In production, consider using a more robust migration strategy instead of auto-creating tables.
// This method is simplified for demonstration purposes and may not cover all edge cases.
// ----------------------------------------------------------------------------
func (p *UserBehaviorPlugin) createTableIfNotExists(ctx context.Context) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id               BIGSERIAL PRIMARY KEY,
			namespace        VARCHAR(255) NOT NULL,
			user_id          VARCHAR(255) NOT NULL,
			event_id         VARCHAR(255) NOT NULL,
			timestamp        VARCHAR(255),
			server_timestamp BIGINT NOT NULL,
			payload          JSONB NOT NULL,
			source_ip        VARCHAR(45),
			created_at       TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_%s_namespace_ts ON %s(namespace, server_timestamp DESC);
		CREATE INDEX IF NOT EXISTS idx_%s_user_id      ON %s(user_id);
		CREATE INDEX IF NOT EXISTS idx_%s_payload      ON %s USING GIN(payload);
	`, p.cfg.Table,
		p.cfg.Table, p.cfg.Table,
		p.cfg.Table, p.cfg.Table,
		p.cfg.Table, p.cfg.Table)

	_, err := p.db.ExecContext(ctx, query)
	return err
}

// Filter determines if an event should be processed by this plugin.
// ------------------------------------------------------------------------------
// DEVELOPER NOTE:
// Implement custom filtering logic here. Return false to skip an event.
// For example, filter out events from certain namespaces or users.
// ------------------------------------------------------------------------------
func (p *UserBehaviorPlugin) Filter(_ *events.UserBehaviorEvent) bool { return true }

// transform converts a UserBehaviorEvent into a row map for Postgres insertion.
// ------------------------------------------------------------------------------
// DEVELOPER NOTE:
// Customize this method to reshape events before storage.
// For example, extract additional fields or apply data masking.
// ------------------------------------------------------------------------------
func (p *UserBehaviorPlugin) transform(e *events.UserBehaviorEvent) (map[string]interface{}, error) {
	doc := e.ToDocument()
	payloadJSON, err := json.Marshal(doc["payload"])
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}
	return map[string]interface{}{
		"namespace":        doc["namespace"],
		"user_id":          doc["user_id"],
		"event_id":         doc["event_id"],
		"timestamp":        doc["timestamp"],
		"server_timestamp": doc["server_timestamp"],
		"payload":          string(payloadJSON),
		"source_ip":        doc["source_ip"],
	}, nil
}

func (p *UserBehaviorPlugin) WriteBatch(ctx context.Context, evts []*events.UserBehaviorEvent) (int, error) {
	if len(evts) == 0 {
		return 0, nil
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	valueStrings := make([]string, 0, len(evts))
	valueArgs := make([]interface{}, 0, len(evts)*7)
	argPos := 1

	for _, e := range evts {
		row, err := p.transform(e)
		if err != nil {
			p.logger.Warn("failed to transform event, skipping", "error", err, "user_id", e.UserID)
			continue
		}
		valueStrings = append(valueStrings,
			fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d)",
				argPos, argPos+1, argPos+2, argPos+3, argPos+4, argPos+5, argPos+6))
		valueArgs = append(valueArgs,
			row["namespace"], row["user_id"], row["event_id"],
			row["timestamp"], row["server_timestamp"], row["payload"], row["source_ip"])
		argPos += 7
	}

	if len(valueStrings) == 0 {
		return 0, fmt.Errorf("all events failed transformation")
	}

	query := fmt.Sprintf(
		`INSERT INTO %s (namespace, user_id, event_id, timestamp, server_timestamp, payload, source_ip) VALUES %s`,
		p.cfg.Table, strings.Join(valueStrings, ","))

	result, err := tx.ExecContext(ctx, query, valueArgs...)
	if err != nil {
		return 0, fmt.Errorf("failed to insert events: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	p.logger.Info("batch written to postgres", "table", p.cfg.Table, "count", rowsAffected)
	return int(rowsAffected), nil
}

func (p *UserBehaviorPlugin) Close() error {
	p.logger.Info("postgres plugin closing", "table", p.cfg.Table)
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}

func (p *UserBehaviorPlugin) HealthCheck(ctx context.Context) error {
	return p.db.PingContext(ctx)
}
