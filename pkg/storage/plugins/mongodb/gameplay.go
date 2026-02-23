// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package mongodb

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/events"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage"
)

// GameplayPluginConfig holds MongoDB configuration for the gameplay plugin.
// Each telemetry type plugin owns its config independently, allowing different
// URIs, databases, or collections per event category.
type GameplayPluginConfig struct {
	URI        string // required, e.g. "mongodb://user:pass@host:27017"
	Database   string // default "telemetry"
	Collection string // default "gameplay_events"
	Workers    int    // connection pool size hint; default 2
}

// GameplayPlugin stores gameplay telemetry events in a MongoDB collection.
// It manages its own mongo.Client and is fully independent of sibling MongoDB plugins.
type GameplayPlugin struct {
	cfg        GameplayPluginConfig
	client     *mongo.Client
	collection *mongo.Collection
	logger     *slog.Logger
}

// NewGameplayPlugin creates a MongoDB plugin for gameplay events.
func NewGameplayPlugin(cfg GameplayPluginConfig) storage.StoragePlugin[*events.GameplayEvent] {
	if cfg.Database == "" {
		cfg.Database = "telemetry"
	}
	if cfg.Collection == "" {
		cfg.Collection = "gameplay_events"
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 2
	}
	return &GameplayPlugin{cfg: cfg}
}

func (p *GameplayPlugin) Name() string { return "mongodb:gameplay" }

func (p *GameplayPlugin) Initialize(ctx context.Context) error {
	p.logger = slog.Default().With("plugin", p.Name())

	if p.cfg.URI == "" {
		return fmt.Errorf("mongodb URI is required")
	}

	poolSize := uint64(p.cfg.Workers * 2)
	if poolSize < 10 {
		poolSize = 10
	}

	clientOpts := options.Client().
		ApplyURI(p.cfg.URI).
		SetMaxPoolSize(poolSize)

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return fmt.Errorf("failed to connect to mongodb: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return fmt.Errorf("failed to ping mongodb: %w", err)
	}

	p.client = client
	p.collection = client.Database(p.cfg.Database).Collection(p.cfg.Collection)

	if err := p.createIndexes(ctx); err != nil {
		return fmt.Errorf("failed to create indexes on %s: %w", p.cfg.Collection, err)
	}

	p.logger.Info("mongodb plugin initialized",
		"database", p.cfg.Database,
		"collection", p.cfg.Collection,
		"pool_size", poolSize)
	return nil
}

func (p *GameplayPlugin) createIndexes(ctx context.Context) error {
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "namespace", Value: 1}, {Key: "server_timestamp", Value: -1}}},
		{Keys: bson.D{{Key: "user_id", Value: 1}}},
		{Keys: bson.D{{Key: "event_id", Value: 1}}},
	}
	opts := options.CreateIndexes().SetMaxTime(30 * time.Second)
	_, err := p.collection.Indexes().CreateMany(ctx, indexes, opts)
	return err
}

// Filter determines if an event should be processed by this plugin.
// ------------------------------------------------------------------------------
// DEVELOPER NOTE:
// Implement custom filtering logic here. Return false to skip an event.
// For example, filter out events from certain namespaces or users.
// ------------------------------------------------------------------------------
func (p *GameplayPlugin) Filter(_ *events.GameplayEvent) bool { return true }

// transform converts a GameplayEvent into a format suitable for MongoDB insertion.
// ------------------------------------------------------------------------------
// DEVELOPER NOTE:
// Customize this method to reshape or enrich events before storage.
// For example, flatten nested properties, convert timestamps, or mask fields.
// ------------------------------------------------------------------------------
func (p *GameplayPlugin) transform(e *events.GameplayEvent) (any, error) {
	flat := e.ToDocument()
	if payload, ok := flat["payload"]; ok {
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal payload: %w", err)
		}
		var payloadMap map[string]interface{}
		if err := json.Unmarshal(payloadBytes, &payloadMap); err != nil {
			return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
		}
		flat["payload"] = payloadMap
	}
	doc := bson.M{"created_at": time.Now()}
	for k, v := range flat {
		doc[k] = v
	}
	return doc, nil
}

func (p *GameplayPlugin) WriteBatch(ctx context.Context, evts []*events.GameplayEvent) (int, error) {
	if len(evts) == 0 {
		return 0, nil
	}

	documents := make([]any, 0, len(evts))
	for _, e := range evts {
		doc, err := p.transform(e)
		if err != nil {
			p.logger.Warn("failed to transform event, skipping", "error", err, "user_id", e.UserID)
			continue
		}
		documents = append(documents, doc)
	}

	if len(documents) == 0 {
		return 0, nil
	}

	opts := options.InsertMany().SetOrdered(false)
	result, err := p.collection.InsertMany(ctx, documents, opts)
	if err != nil {
		if result != nil {
			return len(result.InsertedIDs), err
		}
		return 0, err
	}
	p.logger.Info("batch written to mongodb", "collection", p.cfg.Collection, "count", len(result.InsertedIDs))
	return len(result.InsertedIDs), nil
}

func (p *GameplayPlugin) Close() error {
	p.logger.Info("mongodb plugin closing", "collection", p.cfg.Collection)
	if p.client != nil {
		return p.client.Disconnect(context.Background())
	}
	return nil
}

func (p *GameplayPlugin) HealthCheck(ctx context.Context) error {
	return p.client.Ping(ctx, nil)
}
