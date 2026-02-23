# Plugin Development Guide

This guide helps developers extend the telemetry collector with new storage plugins. There are two common extension scenarios:

1. **[Adding a new telemetry type](#adding-a-new-telemetry-type)** — you have a new event
   category (e.g., economy, combat, social) and want to add plugin support for it across the
   existing storage backends.

2. **[Adding a new storage backend](#adding-a-new-storage-backend)** — you want to route
   events to a new destination (e.g., BigQuery, Elasticsearch, ClickHouse) and need to
   implement plugins for it.

Both scenarios share the same core interface and conventions. Read the
[Plugin Contract](#plugin-contract) section first.

---

## Plugin Contract

Every storage plugin must implement the `StoragePlugin[T]` interface from
`pkg/storage/plugin.go`:

```go
type StoragePlugin[T any] interface {
    Name() string
    Initialize(ctx context.Context) error
    Filter(event T) bool
    WriteBatch(ctx context.Context, events []T) (int, error)
    Close() error
    HealthCheck(ctx context.Context) error
}
```

`T` is the typed event that this plugin handles — one of `*events.UserBehaviorEvent`,
`*events.GameplayEvent`, `*events.PerformanceEvent`, or a new type you define.

### Method responsibilities

| Method | Called by | Responsibility |
|--------|-----------|----------------|
| `Name()` | Logging, diagnostics | Return a unique identifier in the form `"backend:type"` (e.g., `"postgres:gameplay"`) |
| `Initialize(ctx)` | Bootstrap layer, once per plugin | Open connections, validate config, create schemas/tables/indexes. Return error if setup fails. |
| `Filter(event)` | Processor, per event | Return `false` to drop an event before batching; `true` to accept it |
| `WriteBatch(ctx, events)` | Processor, per batch | Transform and write the batch; return the count written and any error |
| `Close()` | Graceful shutdown | Flush pending data and release connections |
| `HealthCheck(ctx)` | Health endpoint | Verify the backend is reachable; return `nil` if healthy |

**Error Handling Best Practice:** During WriteBatch, if individual events fail transformation, log a warning and skip them rather than failing the entire batch:

```go
for _, e := range evts {
    row, err := p.transform(e)
    if err != nil {
        p.logger.Warn("failed to transform event, skipping", "error", err, "user_id", e.UserID)
        continue
    }
    // ... accumulate row ...
}
```

### File layout convention

```
pkg/storage/plugins/
└── <backend>/
    ├── user_behavior.go   // StoragePlugin[*events.UserBehaviorEvent]
    ├── gameplay.go        // StoragePlugin[*events.GameplayEvent]
    └── performance.go     // StoragePlugin[*events.PerformanceEvent]
```

Each file is self-contained: its own config struct, its own connection, its own constructor.
No shared state is expected between plugin files in the same package.

---

## Adding a New Telemetry Type

This section covers the process of adding a new telemetry type. As an example, we'll add an **Economy** telemetry type for tracking in-game purchases and currency transactions.

### Overview of Required Changes

When adding a new telemetry type, you need to modify:
1. **`pkg/events/`** — Create the typed event wrapper
2. **`pkg/storage/plugins/<backend>/`** — Add plugin files for each storage backend
3. **`internal/bootstrap/plugins.go`** — Wire plugins into the application
4. **`pkg/proto/`** — Define protobuf schema (covered in architecture docs)
5. **`pkg/service/`** — Add gRPC service handler (covered in architecture docs)

This guide focuses on items 1-3 which are the plugin layer concerns.

### Step 1 — Create the event wrapper

Create `pkg/events/economy.go`. The wrapper carries server-enriched metadata alongside
the protobuf payload and provides two methods used by the generic infrastructure:

```go
// pkg/events/economy.go

package events

import (
    "fmt"

    pb "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/pb"
)

// EconomyEvent is the typed in-memory representation of an economy telemetry event.
// It carries server-enriched metadata alongside the raw protobuf payload.
type EconomyEvent struct {
    Namespace       string
    UserID          string
    ServerTimestamp int64
    SourceIP        string
    Payload         *pb.CreateEconomyTelemetryRequest // the raw protobuf message
}

// DeduplicationKey returns a stable string used to identify duplicate events.
// Adjust the fields to match what makes an economy event unique in your domain.
func (e *EconomyEvent) DeduplicationKey() string {
    eventID := ""
    if e.Payload != nil {
        eventID = e.Payload.EventId
    }
    return fmt.Sprintf("economy:%s:%s:%s", e.Namespace, e.UserID, eventID)
}

// ToDocument returns the event as a flat map suitable for JSON/BSON serialization.
// Add or remove fields here to control what is stored by all backends.
func (e *EconomyEvent) ToDocument() map[string]interface{} {
    doc := map[string]interface{}{
        "kind":             "economy",
        "namespace":        e.Namespace,
        "user_id":          e.UserID,
        "server_timestamp": e.ServerTimestamp,
        "source_ip":        e.SourceIP,
    }
    if e.Payload != nil {
        doc["event_id"]  = e.Payload.EventId
        doc["timestamp"] = e.Payload.Timestamp
        doc["payload"]   = e.Payload
    }
    return doc
}
```

`DeduplicationKey()` and `ToDocument()` are the only two methods that the generic
infrastructure (`Processor`, `Deduplicator`) requires from an event type. Everything else
is up to the plugin author.

### Step 2 — Create plugin files

For each storage backend you want to support, add an `economy.go` file. The structure
mirrors the existing files exactly — only the type names and event-specific fields change.

Below is a complete example for the **Postgres** backend:

```go
// pkg/storage/plugins/postgres/economy.go

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

// EconomyPluginConfig holds Postgres configuration for the economy plugin.
// Each telemetry type plugin owns its config independently, allowing different
// DSNs, tables, or pool sizes per event category.
type EconomyPluginConfig struct {
    DSN     string // required
    Table   string // default "economy_events"
    Workers int    // connection pool size; default 2
}

// EconomyPlugin stores economy telemetry events in a PostgreSQL table.
// It manages its own database connection and is fully independent of sibling Postgres plugins.
type EconomyPlugin struct {
    cfg    EconomyPluginConfig
    db     *sql.DB
    logger *slog.Logger
}

// NewEconomyPlugin creates a Postgres plugin for economy events.
func NewEconomyPlugin(cfg EconomyPluginConfig) storage.StoragePlugin[*events.EconomyEvent] {
    if cfg.Table == "" {
        cfg.Table = "economy_events"
    }
    if cfg.Workers <= 0 {
        cfg.Workers = 2
    }
    return &EconomyPlugin{cfg: cfg}
}

func (p *EconomyPlugin) Name() string { return "postgres:economy" }

func (p *EconomyPlugin) Initialize(ctx context.Context) error {
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

func (p *EconomyPlugin) createTableIfNotExists(ctx context.Context) error {
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
func (p *EconomyPlugin) Filter(_ *events.EconomyEvent) bool { return true }

// transform converts an EconomyEvent into a row map for Postgres insertion.
// ------------------------------------------------------------------------------
// DEVELOPER NOTE:
// Customize this method to reshape events before storage.
// For example, extract additional fields or apply data masking.
// ------------------------------------------------------------------------------
func (p *EconomyPlugin) transform(e *events.EconomyEvent) (map[string]interface{}, error) {
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

func (p *EconomyPlugin) WriteBatch(ctx context.Context, evts []*events.EconomyEvent) (int, error) {
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

func (p *EconomyPlugin) Close() error {
    p.logger.Info("postgres plugin closing", "table", p.cfg.Table)
    if p.db != nil {
        return p.db.Close()
    }
    return nil
}

func (p *EconomyPlugin) HealthCheck(ctx context.Context) error {
    return p.db.PingContext(ctx)
}
```

Repeat the same structure for any other backend (`kafka/economy.go`, `mongodb/economy.go`,
`s3/economy.go`), adjusting only the connection type and the write logic. The config struct,
the `Filter` method, and the `transform`/`WriteBatch` skeleton are the same in every file.

### Step 3 — Wire into bootstrap layer

**Critical:** Plugins are registered in `internal/bootstrap/plugins.go`, NOT in `main.go`. You need to:

1. Add the new event type to the `StoragePlugins` struct
2. Add initialization logic in the `InitializeStoragePlugins` function
3. Add cleanup logic in the `CloseStoragePlugins` function
4. Add processor initialization in `internal/bootstrap/processor.go`

#### A. Update the StoragePlugins struct

```go
// internal/bootstrap/plugins.go

// StoragePlugins holds all initialized storage plugins for each event type
type StoragePlugins struct {
	UserBehavior []storage.StoragePlugin[*events.UserBehaviorEvent]
	Gameplay     []storage.StoragePlugin[*events.GameplayEvent]
	Performance  []storage.StoragePlugin[*events.PerformanceEvent]
	Economy      []storage.StoragePlugin[*events.EconomyEvent]  // ADD THIS
}
```

#### B. Add initialization in InitializeStoragePlugins

Inside the `InitializeStoragePlugins` function, add your economy plugin to each backend case:

```go
// internal/bootstrap/plugins.go

func InitializeStoragePlugins(ctx context.Context, appCfg *config.Config, logger *slog.Logger) (*StoragePlugins, error) {
	plugins := &StoragePlugins{
		UserBehavior: []storage.StoragePlugin[*events.UserBehaviorEvent]{},
		Gameplay:     []storage.StoragePlugin[*events.GameplayEvent]{},
		Performance:  []storage.StoragePlugin[*events.PerformanceEvent]{},
		Economy:      []storage.StoragePlugin[*events.EconomyEvent]{},  // ADD THIS
	}

	for _, pluginName := range appCfg.GetEnabledPlugins() {
		var (
			ubPlugin   storage.StoragePlugin[*events.UserBehaviorEvent]
			gpPlugin   storage.StoragePlugin[*events.GameplayEvent]
			perfPlugin storage.StoragePlugin[*events.PerformanceEvent]
			econPlugin storage.StoragePlugin[*events.EconomyEvent]  // ADD THIS
		)

		switch pluginName {
		case "postgres":
			// ... existing plugins ...
			econPlugin = postgres.NewEconomyPlugin(postgres.EconomyPluginConfig{
				DSN:     appCfg.Storage.Postgres.PostgresDSN,
				Table:   "economy_events",
				Workers: appCfg.Storage.Postgres.Workers,
			})

		case "s3":
			// ... existing plugins ...
			econPlugin = s3.NewEconomyPlugin(s3.EconomyPluginConfig{
				Bucket:   appCfg.Storage.S3.S3Bucket,
				Prefix:   appCfg.Storage.S3.S3Prefix,
				Region:   appCfg.Storage.S3.S3Region,
				Endpoint: appCfg.Storage.S3.S3Endpoint,
			})

		case "noop":
			// ... existing plugins ...
			econPlugin = noop.NewNoopPlugin[*events.EconomyEvent]()

		// Add for other backends as needed
		}

		// Initialize the plugin
		if err := econPlugin.Initialize(ctx); err != nil {
			return nil, err
		}
		logger.Info("plugin initialized", "plugin", econPlugin.Name())

		// Add to the plugins list
		plugins.Economy = append(plugins.Economy, econPlugin)  // ADD THIS
		// ... existing appends for ubPlugin, gpPlugin, perfPlugin ...
	}

	// Update the noop fallback logic
	if len(plugins.UserBehavior) == 0 {
		// ... existing noop initialization ...
		noopEcon := noop.NewNoopPlugin[*events.EconomyEvent]()
		noopEcon.Initialize(ctx)
		plugins.Economy = append(plugins.Economy, noopEcon)  // ADD THIS
	}

	logger.Info("storage plugins ready",
		"user_behavior", len(plugins.UserBehavior),
		"gameplay", len(plugins.Gameplay),
		"performance", len(plugins.Performance),
		"economy", len(plugins.Economy))  // ADD THIS

	return plugins, nil
}
```

#### C. Add cleanup in CloseStoragePlugins

```go
// internal/bootstrap/plugins.go

func CloseStoragePlugins(plugins *StoragePlugins, logger *slog.Logger) {
	// ... existing close logic for other types ...
	
	for _, p := range plugins.Economy {  // ADD THIS
		if err := p.Close(); err != nil {
			logger.Error("failed to close plugin", "plugin", p.Name(), "error", err)
		}
	}
}
```

#### D. Add processor initialization

Create `internal/bootstrap/processor.go` modifications:

```go
// internal/bootstrap/processor.go

type Processors struct {
	UserBehavior *processor.Processor[*events.UserBehaviorEvent]
	Gameplay     *processor.Processor[*events.GameplayEvent]
	Performance  *processor.Processor[*events.PerformanceEvent]
	Economy      *processor.Processor[*events.EconomyEvent]  // ADD THIS
}

func InitializeProcessors(
	cfg *config.Config,
	plugins *StoragePlugins,
	dedups *Deduplicators,
	logger *slog.Logger,
) *Processors {
	processorCfg := processor.Config{
		Workers:       cfg.Processor.Workers,
		ChannelBuffer: cfg.Processor.ChannelBuffer,
		BatchSize:     cfg.Processor.DefaultBatchSize,
		FlushInterval: cfg.Processor.DefaultFlushInterval,
	}

	// ... existing processors ...

	// ADD THIS:
	economyProc := processor.NewProcessor(processorCfg, plugins.Economy, dedups.Economy, logger)
	economyProc.Start()

	return &Processors{
		UserBehavior: ubProc,
		Gameplay:     gpProc,
		Performance:  perfProc,
		Economy:      economyProc,  // ADD THIS
	}
}

func ShutdownProcessors(procs *Processors, timeout time.Duration, logger *slog.Logger) {
	// ... existing shutdown logic ...
	procs.Economy.Stop(timeout)  // ADD THIS
}
```

#### E. Add deduplicator initialization

```go
// internal/bootstrap/deduplicator.go

type Deduplicators struct {
	UserBehavior dedup.Deduplicator[*events.UserBehaviorEvent]
	Gameplay     dedup.Deduplicator[*events.GameplayEvent]
	Performance  dedup.Deduplicator[*events.PerformanceEvent]
	Economy      dedup.Deduplicator[*events.EconomyEvent]  // ADD THIS
}

func InitializeDeduplicators(cfg *config.Config, logger *slog.Logger) *Deduplicators {
	// ... existing logic ...
	return &Deduplicators{
		UserBehavior: ubDedup,
		Gameplay:     gpDedup,
		Performance:  perfDedup,
		Economy:      buildDeduplicator[*events.EconomyEvent](cfg, logger),  // ADD THIS
	}
}
```

The `Processor`, `Batcher`, and `Deduplicator` are all generic — they automatically handle
any event type that implements `DeduplicationKey()` without needing changes.

---

## Adding a New Storage Backend

This section shows how to add a completely new storage backend. We'll use **Google BigQuery**
as the example.

### Overview

Adding a new storage backend requires:
1. Creating plugin files in `pkg/storage/plugins/<backend>/`
2. Wiring the backend in `internal/bootstrap/plugins.go`
3. Adding configuration in `pkg/config/config.go`
4. (Optional) Adding environment variables documentation

Let's walk through each step.

### Step 1 — Add the dependency

Add the necessary Go module dependency:

```bash
go get cloud.google.com/go/bigquery
```

### Step 2 — Create the package and plugin files

Create a directory `pkg/storage/plugins/bigquery/`. Add one file per telemetry type.
Below is a complete implementation for [gameplay.go](gameplay.go):

```go
// pkg/storage/plugins/bigquery/gameplay.go

package bigquery

import (
    "context"
    "encoding/json"
    "fmt"

    "cloud.google.com/go/bigquery"

    "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/events"
    "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage"
)

// GameplayPluginConfig holds BigQuery configuration for the gameplay plugin.
// Each telemetry type plugin owns its config independently, allowing different
// projects, datasets, or tables per event category.
type GameplayPluginConfig struct {
    ProjectID string // required
    DatasetID string // required
    TableID   string // default "gameplay_events"
}

// gameplayRow defines the BigQuery schema via struct field tags.
// Adjust the fields to match what you want stored in BigQuery.
type gameplayRow struct {
    Namespace       string `bigquery:"namespace"`
    UserID          string `bigquery:"user_id"`
    EventID         string `bigquery:"event_id"`
    Timestamp       string `bigquery:"timestamp"`
    ServerTimestamp int64  `bigquery:"server_timestamp"`
    Payload         string `bigquery:"payload"` // JSON-encoded string
    SourceIP        string `bigquery:"source_ip"`
}

// GameplayPlugin streams gameplay telemetry events to a BigQuery table via the
// streaming insert API. It manages its own BigQuery client and is fully
// independent of sibling BigQuery plugins.
type GameplayPlugin struct {
    cfg      GameplayPluginConfig
    client   *bigquery.Client
    inserter *bigquery.Inserter
    logger   *slog.Logger
}

// NewGameplayPlugin creates a BigQuery plugin for gameplay events.
func NewGameplayPlugin(cfg GameplayPluginConfig) storage.StoragePlugin[*events.GameplayEvent] {
    if cfg.TableID == "" {
        cfg.TableID = "gameplay_events"
    }
    return &GameplayPlugin{cfg: cfg}
}

func (p *GameplayPlugin) Name() string { 
    // name your plugin here
 }

func (p *GameplayPlugin) Initialize(ctx context.Context) error {
    // initialize the BigQuery client here
}

func (p *GameplayPlugin) Filter(e *events.GameplayEvent) bool { 
    // implement custom filtering logic here. For example:
    // if e.Platform.Mobile {
    //     return false // skip mobile events for this plugin
    // }
    // return true 
}

func (p *GameplayPlugin) transform(e *events.GameplayEvent) (gameplayRow, error) {
    // implement transformation logic here, e.g., JSON-encode the payload
}

func (p *GameplayPlugin) WriteBatch(ctx context.Context, evts []*events.GameplayEvent) (int, error) {
    // implement batch writing logic here using p.inserter.Put(ctx, rows)
}

func (p *GameplayPlugin) Close() error {
    if p.client != nil {
        return p.client.Close()
    }
    return nil
}

func (p *GameplayPlugin) HealthCheck(ctx context.Context) error {
    _, err := p.client.Dataset(p.cfg.DatasetID).Metadata(ctx)
    if err != nil {
        return fmt.Errorf("bigquery health check failed: %w", err)
    }
    return nil
}
```

Create `user_behavior.go` and `performance.go` in the same package following the same
structure, substituting `UserBehaviorEvent`/`PerformanceEvent` for `GameplayEvent` and
naming the row struct and plugin accordingly.

### Step 3 — Add configuration

Add BigQuery configuration to `pkg/config/config.go`:

```go
// pkg/config/config.go

type StorageConfig struct {
    Postgres PostgresConfig `envPrefix:"POSTGRES_"`
    S3       S3Config       `envPrefix:"S3_"`
    Kafka    KafkaConfig    `envPrefix:"KAFKA_"`
    MongoDB  MongoDBConfig  `envPrefix:"MONGODB_"`
    BigQuery BigQueryConfig `envPrefix:"BIGQUERY_"`  // ADD THIS
    Plugins  string         `env:"PLUGINS" envDefault:""`
}

// ADD THIS struct:
type BigQueryConfig struct {
    ProjectID string `env:"PROJECT_ID"`
    DatasetID string `env:"DATASET_ID"`
}
```

Now you can set environment variables like:
- `STORAGE_BIGQUERY_PROJECT_ID=my-project`
- `STORAGE_BIGQUERY_DATASET_ID=telemetry_data`

### Step 4 — Wire into bootstrap layer

Add the `bigquery` case to the plugin switch in `internal/bootstrap/plugins.go`:

```go
// internal/bootstrap/plugins.go

import (
    // ... existing imports ...
    "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage/plugins/bigquery"
)

func InitializeStoragePlugins(ctx context.Context, appCfg *config.Config, logger *slog.Logger) (*StoragePlugins, error) {
    // ... existing code ...

    for _, pluginName := range appCfg.GetEnabledPlugins() {
        // ... existing variable declarations ...

        switch pluginName {
        // ... existing cases for postgres, s3, kafka, mongodb, noop ...

        case "bigquery":  // ADD THIS CASE
            ubPlugin = bigquery.NewUserBehaviorPlugin(bigquery.UserBehaviorPluginConfig{
                ProjectID: appCfg.Storage.BigQuery.ProjectID,
                DatasetID: appCfg.Storage.BigQuery.DatasetID,
                TableID:   "user_behavior_events",
            })
            gpPlugin = bigquery.NewGameplayPlugin(bigquery.GameplayPluginConfig{
                ProjectID: appCfg.Storage.BigQuery.ProjectID,
                DatasetID: appCfg.Storage.BigQuery.DatasetID,
                TableID:   "gameplay_events",
            })
            perfPlugin = bigquery.NewPerformancePlugin(bigquery.PerformancePluginConfig{
                ProjectID: appCfg.Storage.BigQuery.ProjectID,
                DatasetID: appCfg.Storage.BigQuery.DatasetID,
                TableID:   "performance_events",
            })

        default:
            logger.Error("unknown plugin", "plugin", pluginName)
            continue
        }

        // ... rest of initialization code ...
    }

    return plugins, nil
}
```

### Step 5 — Enable the plugin

Set the environment variable to enable your new backend:

```bash
export STORAGE_PLUGINS=bigquery
# or combine with existing: STORAGE_PLUGINS=postgres,bigquery
```

Create `user_behavior.go` and `performance.go` in the same package following the same
structure, substituting `UserBehaviorEvent`/`PerformanceEvent` for `GameplayEvent` and
naming the row struct and plugin accordingly.

---

## Quick Reference

### Files to Modify When Adding a New Telemetry Type

1. **`pkg/events/<type>.go`** — Event wrapper with `DeduplicationKey()` and `ToDocument()`
2. **`pkg/storage/plugins/<backend>/<type>.go`** — One file per backend
3. **`internal/bootstrap/plugins.go`** — Add to `StoragePlugins` struct and initialization
4. **`internal/bootstrap/processor.go`** — Add processor for the new type
5. **`internal/bootstrap/deduplicator.go`** — Add deduplicator for the new type

### Files to Modify When Adding a New Storage Backend

1. **`pkg/storage/plugins/<backend>/<type>.go`** — Create plugin files (one per telemetry type)
2. **`pkg/config/config.go`** — Add config struct for the backend
3. **`internal/bootstrap/plugins.go`** — Add case in the plugin switch

### Testing Your Plugin

```bash
# Set environment variables
export STORAGE_PLUGINS=<your-backend>
export STORAGE_<BACKEND>_<CONFIG_KEY>=<value>

# Run the service
go run main.go

# Check logs for plugin initialization
# Look for: "plugin initialized" log entries with your plugin name
```

---
