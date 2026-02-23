# Plugin Development Guide

This guide helps developers extend the telemetry collector with new storage plugins. There are two common extension scenarios:

1. **[Handling a new AGS event type](#handling-a-new-ags-event-type)** — AGS published a new event you want to capture (e.g., achievement unlocked, matchmaking completed) and you want to add storage plugin support for it across existing backends.

2. **[Adding a new storage backend](#adding-a-new-storage-backend)** — you want to route events to a new destination (e.g., BigQuery, Elasticsearch, ClickHouse) and need to implement plugins for it.

Both scenarios share the same core interface and conventions. Read the [Plugin Contract](#plugin-contract) section first.

> **Important:** This is an Extend Event Handler app. Event schemas are defined by AccelByte Gaming Services — you cannot define your own proto messages. All proto files must come from the [AGS event catalogue](https://docs.accelbyte.io/gaming-services/knowledge-base/api-events/) (source: [accelbyte-api-proto on GitHub](https://github.com/AccelByte/accelbyte-api-proto/tree/main/asyncapi)).

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

`T` is the typed event that this plugin handles — for example `*events.StatItemUpdatedEvent`, or a new type you define for another AGS event.

### Method responsibilities

| Method | Called by | Responsibility |
|--------|-----------|----------------|
| `Name()` | Logging, diagnostics | Return a unique identifier in the form `"backend:type"` (e.g., `"postgres:stat_item_updated"`) |
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
    └── stat_item_updated.go   // StoragePlugin[*events.StatItemUpdatedEvent]
    // one file per AGS event type you handle
```

Each file is self-contained: its own config struct, its own connection, its own constructor.
No shared state is expected between plugin files in the same package.

---

## Handling a New AGS Event Type

This section covers the full process of handling a new AGS event. As an example, we'll add handling for the **`StatItemDeleted`** event from the Statistic service.

### Overview of Required Changes

1. **`pkg/proto/accelbyte-asyncapi/`** — Copy the AGS proto file and generate Go code
2. **`pkg/events/`** — Create the typed event wrapper
3. **`pkg/service/`** — Add the gRPC service handler
4. **`pkg/storage/plugins/<backend>/`** — Add plugin files for each storage backend
5. **`internal/bootstrap/plugins.go`** — Add to `StoragePlugins` struct and initialization
6. **`internal/bootstrap/processor.go`** — Add processor for the new type
7. **`internal/bootstrap/deduplicator.go`** — Add deduplicator for the new type
8. **`internal/app/app.go`** — Register the new gRPC service

### Step 1 — Get the proto definition

All AGS event proto files are published at:
**https://github.com/AccelByte/accelbyte-api-proto/tree/main/asyncapi**

Find the service that owns the event you want to handle. For the Statistic service, the proto is already present at:

```
pkg/proto/accelbyte-asyncapi/social/statistic/v1/statistic.proto
```

If you need a proto from a different AGS service, copy it from the repository into the matching path under `pkg/proto/accelbyte-asyncapi/`, then generate Go code:

```bash
# From the repository root
./proto.sh
```

This regenerates all `*.pb.go` and `*_grpc.pb.go` files under `pkg/pb/accelbyte-asyncapi/`.

### Step 2 — Create the event wrapper

Create `pkg/events/stat_item_deleted.go`. The wrapper carries server-enriched metadata alongside the decoded event fields and provides two methods used by the generic infrastructure:

```go
// pkg/events/stat_item_deleted.go

package events

import "fmt"

// StatItemDeletedEvent is the typed in-memory representation of a stat item deleted event.
type StatItemDeletedEvent struct {
    ID              string
    Version         int64
    Namespace       string
    Name            string
    UserID          string
    SessionID       string
    Timestamp       string
    ServerTimestamp int64
    Payload         *StatItem // reuse StatItem from stat_item_updated.go
}

// DeduplicationKey returns a stable string used to identify duplicate events.
func (e *StatItemDeletedEvent) DeduplicationKey() string {
    statCode := ""
    if e.Payload != nil {
        statCode = e.Payload.StatCode
    }
    return fmt.Sprintf("stat_item_deleted:%s:%s:%s:%s", e.Namespace, e.UserID, e.Timestamp, statCode)
}

// ToDocument returns the event as a flat map suitable for JSON/BSON serialization.
func (e *StatItemDeletedEvent) ToDocument() map[string]interface{} {
    doc := map[string]interface{}{
        "kind":             "stat_item_deleted",
        "namespace":        e.Namespace,
        "user_id":          e.UserID,
        "server_timestamp": e.ServerTimestamp,
    }
    if e.Payload != nil {
        doc["event_id"]   = e.ID
        doc["event_name"] = e.Name
        doc["version"]    = e.Version
        doc["timestamp"]  = e.Timestamp
        doc["session_id"] = e.SessionID
        doc["payload"]    = map[string]interface{}{
            "stat_code":    e.Payload.StatCode,
            "user_id":      e.Payload.UserId,
            "latest_value": e.Payload.LatestValue,
        }
    }
    return doc
}
```

`DeduplicationKey()` and `ToDocument()` are the only two methods the generic infrastructure (`Processor`, `Deduplicator`) requires. Everything else is up to the plugin author.

### Step 3 — Create the gRPC service handler

Create `pkg/service/stat_item_deleted.go`. This implements the generated server interface for the event:

```go
// pkg/service/stat_item_deleted.go

package service

import (
    "context"
    "log/slog"
    "time"

    "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/events"
    statistic "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/pb/accelbyte-asyncapi/social/statistic/v1"
    "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/processor"

    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "google.golang.org/protobuf/types/known/emptypb"
)

type StatItemDeletedService struct {
    statistic.UnimplementedStatisticStatItemDeletedServiceServer
    namespace string
    proc      *processor.Processor[*events.StatItemDeletedEvent]
    logger    *slog.Logger
}

func NewStatItemDeletedService(
    namespace string,
    proc *processor.Processor[*events.StatItemDeletedEvent],
    logger *slog.Logger,
) *StatItemDeletedService {
    return &StatItemDeletedService{
        namespace: namespace,
        proc:      proc,
        logger:    logger.With("component", "stat_item_deleted_service"),
    }
}

func (s *StatItemDeletedService) OnMessage(ctx context.Context, req *statistic.StatItemDeleted) (*emptypb.Empty, error) {
    if req == nil {
        return nil, status.Error(codes.InvalidArgument, "request is required")
    }

    namespace := req.Namespace
    if namespace == "" {
        namespace = s.namespace
    }

    event := &events.StatItemDeletedEvent{
        ID:              req.Id,
        Name:            req.Name,
        Namespace:       namespace,
        UserID:          req.UserId,
        SessionID:       req.SessionId,
        Timestamp:       req.Timestamp,
        ServerTimestamp: time.Now().UnixMilli(),
        Payload: &events.StatItem{
            StatCode:    req.Payload.GetStatCode(),
            UserId:      req.Payload.GetUserId(),
            LatestValue: req.Payload.GetLatestValue(),
        },
    }

    if err := s.proc.Submit(event); err != nil {
        s.logger.Error("failed to submit stat_item_deleted event",
            "error", err, "namespace", event.Namespace, "user_id", event.UserID)
        return nil, status.Error(codes.Internal, "failed to process event")
    }
    return &emptypb.Empty{}, nil
}
```

### Step 4 — Create plugin files

For each storage backend you want to support, add a plugin file. Use the existing `stat_item_updated.go` files as your template — only the type names and event-specific fields change.

Below is a complete example for the **Postgres** backend:

```go
// pkg/storage/plugins/postgres/stat_item_deleted.go

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

type StatItemDeletedPluginConfig struct {
    DSN     string
    Table   string // default "stat_item_deleted_events"
    Workers int
}

type StatItemDeletedPlugin struct {
    cfg    StatItemDeletedPluginConfig
    db     *sql.DB
    logger *slog.Logger
}

func NewStatItemDeletedPlugin(cfg StatItemDeletedPluginConfig) storage.StoragePlugin[*events.StatItemDeletedEvent] {
    if cfg.Table == "" {
        cfg.Table = "stat_item_deleted_events"
    }
    if cfg.Workers <= 0 {
        cfg.Workers = 2
    }
    return &StatItemDeletedPlugin{cfg: cfg}
}

func (p *StatItemDeletedPlugin) Name() string { return "postgres:stat_item_deleted" }

func (p *StatItemDeletedPlugin) Initialize(ctx context.Context) error {
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

func (p *StatItemDeletedPlugin) createTableIfNotExists(ctx context.Context) error {
    query := fmt.Sprintf(`
        CREATE TABLE IF NOT EXISTS %s (
            id               BIGSERIAL PRIMARY KEY,
            namespace        VARCHAR(255) NOT NULL,
            user_id          VARCHAR(255) NOT NULL,
            event_id         VARCHAR(255) NOT NULL,
            timestamp        VARCHAR(255),
            server_timestamp BIGINT NOT NULL,
            payload          JSONB NOT NULL,
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
func (p *StatItemDeletedPlugin) Filter(_ *events.StatItemDeletedEvent) bool { return true }

// transform converts a StatItemDeletedEvent into a row map for Postgres insertion.
// ------------------------------------------------------------------------------
// DEVELOPER NOTE:
// Customize this method to reshape events before storage.
// For example, extract additional fields or apply data masking.
// ------------------------------------------------------------------------------
func (p *StatItemDeletedPlugin) transform(e *events.StatItemDeletedEvent) (map[string]interface{}, error) {
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
    }, nil
}

func (p *StatItemDeletedPlugin) WriteBatch(ctx context.Context, evts []*events.StatItemDeletedEvent) (int, error) {
    if len(evts) == 0 {
        return 0, nil
    }
    tx, err := p.db.BeginTx(ctx, nil)
    if err != nil {
        return 0, fmt.Errorf("failed to begin transaction: %w", err)
    }
    defer tx.Rollback()

    valueStrings := make([]string, 0, len(evts))
    valueArgs := make([]interface{}, 0, len(evts)*6)
    argPos := 1

    for _, e := range evts {
        row, err := p.transform(e)
        if err != nil {
            p.logger.Warn("failed to transform event, skipping", "error", err, "user_id", e.UserID)
            continue
        }
        valueStrings = append(valueStrings,
            fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d)", argPos, argPos+1, argPos+2, argPos+3, argPos+4, argPos+5))
        valueArgs = append(valueArgs,
            row["namespace"], row["user_id"], row["event_id"],
            row["timestamp"], row["server_timestamp"], row["payload"])
        argPos += 6
    }
    if len(valueStrings) == 0 {
        return 0, fmt.Errorf("all events failed transformation")
    }
    query := fmt.Sprintf(
        `INSERT INTO %s (namespace, user_id, event_id, timestamp, server_timestamp, payload) VALUES %s`,
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

func (p *StatItemDeletedPlugin) Close() error {
    p.logger.Info("postgres plugin closing", "table", p.cfg.Table)
    if p.db != nil {
        return p.db.Close()
    }
    return nil
}

func (p *StatItemDeletedPlugin) HealthCheck(ctx context.Context) error {
    return p.db.PingContext(ctx)
}
```

Repeat the same structure for any other backend (`kafka/stat_item_deleted.go`, `mongodb/stat_item_deleted.go`, `s3/stat_item_deleted.go`), adjusting only the connection type and write logic. Use the corresponding `stat_item_updated.go` in each backend package as your template.

### Step 5 — Wire into bootstrap layer

**Critical:** Plugins are registered in `internal/bootstrap/plugins.go`, NOT in `main.go`.

#### A. Update the StoragePlugins struct

```go
// internal/bootstrap/plugins.go

type StoragePlugins struct {
    StatItemUpdated []storage.StoragePlugin[*events.StatItemUpdatedEvent]
    StatItemDeleted []storage.StoragePlugin[*events.StatItemDeletedEvent]  // ADD THIS
}
```

#### B. Add initialization in InitializeStoragePlugins

```go
plugins := &StoragePlugins{
    StatItemUpdated: []storage.StoragePlugin[*events.StatItemUpdatedEvent]{},
    StatItemDeleted: []storage.StoragePlugin[*events.StatItemDeletedEvent]{},  // ADD THIS
}

for _, pluginName := range appCfg.GetEnabledPlugins() {
    var (
        siuPlugin storage.StoragePlugin[*events.StatItemUpdatedEvent]
        sidPlugin storage.StoragePlugin[*events.StatItemDeletedEvent]  // ADD THIS
    )

    switch pluginName {
    case "postgres":
        siuPlugin = postgres.NewStatItemUpdatedPlugin(...)
        sidPlugin = postgres.NewStatItemDeletedPlugin(postgres.StatItemDeletedPluginConfig{  // ADD THIS
            DSN:     appCfg.Storage.Postgres.PostgresDSN,
            Table:   "stat_item_deleted_events",
            Workers: appCfg.Storage.Postgres.Workers,
        })
    // ... repeat for s3, kafka, mongodb, noop cases ...
    }

    // Initialize and append
    if err := sidPlugin.Initialize(ctx); err != nil {
        return nil, err
    }
    logger.Info("plugin initialized", "plugin", sidPlugin.Name())
    plugins.StatItemDeleted = append(plugins.StatItemDeleted, sidPlugin)
}

// Noop fallback
if len(plugins.StatItemUpdated) == 0 {
    // ... existing noop initialization ...
    noopSID := noop.NewNoopPlugin[*events.StatItemDeletedEvent]()
    noopSID.Initialize(ctx)
    plugins.StatItemDeleted = append(plugins.StatItemDeleted, noopSID)
}
```

#### C. Add cleanup in CloseStoragePlugins

```go
for _, p := range plugins.StatItemDeleted {
    if err := p.Close(); err != nil {
        logger.Error("failed to close plugin", "plugin", p.Name(), "error", err)
    }
}
```

#### D. Add processor initialization (`internal/bootstrap/processor.go`)

```go
type Processors struct {
    StatItemUpdated *processor.Processor[*events.StatItemUpdatedEvent]
    StatItemDeleted *processor.Processor[*events.StatItemDeletedEvent]  // ADD THIS
}

func InitializeProcessors(...) *Processors {
    procs := &Processors{
        StatItemUpdated: processor.NewProcessor(processorCfg, plugins.StatItemUpdated, dedups.StatItemUpdated, logger),
        StatItemDeleted: processor.NewProcessor(processorCfg, plugins.StatItemDeleted, dedups.StatItemDeleted, logger),  // ADD THIS
    }
    procs.StatItemUpdated.Start()
    procs.StatItemDeleted.Start()  // ADD THIS
    return procs
}

func ShutdownProcessors(procs *Processors, timeout time.Duration, logger *slog.Logger) {
    // ... existing ...
    if err := procs.StatItemDeleted.Shutdown(timeout); err != nil {  // ADD THIS
        logger.Error("stat_item_deleted processor shutdown error", "error", err)
    }
}
```

#### E. Add deduplicator initialization (`internal/bootstrap/deduplicator.go`)

```go
type Deduplicators struct {
    StatItemUpdated dedup.Deduplicator[*events.StatItemUpdatedEvent]
    StatItemDeleted dedup.Deduplicator[*events.StatItemDeletedEvent]  // ADD THIS
}

func InitializeDeduplicators(appCfg *config.Config, logger *slog.Logger) *Deduplicators {
    return &Deduplicators{
        StatItemUpdated: buildDeduplicator[*events.StatItemUpdatedEvent](appCfg, logger),
        StatItemDeleted: buildDeduplicator[*events.StatItemDeletedEvent](appCfg, logger),  // ADD THIS
    }
}

func CloseDeduplicators(dedups *Deduplicators, logger *slog.Logger) {
    // ... existing ...
    if err := dedups.StatItemDeleted.Close(); err != nil {  // ADD THIS
        logger.Error("stat_item_deleted deduplicator close error", "error", err)
    }
}
```

#### F. Register the gRPC service (`internal/app/app.go`)

```go
statItemDeletedSvc := service.NewStatItemDeletedService(
    bootstrap.GetNamespace(),
    app.processors.StatItemDeleted,
    app.logger,
)
statistic.RegisterStatisticStatItemDeletedServiceServer(grpcServer, statItemDeletedSvc)
```

The `Processor`, `Batcher`, and `Deduplicator` are all generic — they automatically handle any event type that implements `DeduplicationKey()` without needing changes.

---

## Adding a New Storage Backend

This section shows how to add a completely new storage backend. We'll use **Google BigQuery** as the example.

### Overview

Adding a new storage backend requires:
1. Creating plugin files in `pkg/storage/plugins/<backend>/`
2. Adding configuration in `pkg/config/config.go`
3. Wiring the backend in `internal/bootstrap/plugins.go`

### Step 1 — Add the dependency

```bash
go get cloud.google.com/go/bigquery
```

### Step 2 — Create the plugin package

Create a directory `pkg/storage/plugins/bigquery/`. Add one file per event type you handle. Below is a skeleton for `stat_item_updated.go`:

```go
// pkg/storage/plugins/bigquery/stat_item_updated.go

package bigquery

import (
    "context"
    "fmt"
    "log/slog"

    "cloud.google.com/go/bigquery"

    "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/events"
    "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage"
)

type StatItemUpdatedPluginConfig struct {
    ProjectID string // required
    DatasetID string // required
    TableID   string // default "stat_item_updated_events"
}

type statItemUpdatedRow struct {
    Namespace       string `bigquery:"namespace"`
    UserID          string `bigquery:"user_id"`
    EventID         string `bigquery:"event_id"`
    Timestamp       string `bigquery:"timestamp"`
    ServerTimestamp int64  `bigquery:"server_timestamp"`
    Payload         string `bigquery:"payload"` // JSON-encoded
}

type StatItemUpdatedPlugin struct {
    cfg      StatItemUpdatedPluginConfig
    client   *bigquery.Client
    inserter *bigquery.Inserter
    logger   *slog.Logger
}

func NewStatItemUpdatedPlugin(cfg StatItemUpdatedPluginConfig) storage.StoragePlugin[*events.StatItemUpdatedEvent] {
    if cfg.TableID == "" {
        cfg.TableID = "stat_item_updated_events"
    }
    return &StatItemUpdatedPlugin{cfg: cfg}
}

func (p *StatItemUpdatedPlugin) Name() string { return "bigquery:stat_item_updated" }

func (p *StatItemUpdatedPlugin) Initialize(ctx context.Context) error {
    p.logger = slog.Default().With("plugin", p.Name())
    // initialize BigQuery client and inserter here
    return nil
}

// Filter determines if an event should be processed by this plugin.
// ------------------------------------------------------------------------------
// DEVELOPER NOTE:
// Implement custom filtering logic here. Return false to skip an event.
// For example, filter out events from certain namespaces or users.
// ------------------------------------------------------------------------------
func (p *StatItemUpdatedPlugin) Filter(_ *events.StatItemUpdatedEvent) bool { return true }

func (p *StatItemUpdatedPlugin) WriteBatch(ctx context.Context, evts []*events.StatItemUpdatedEvent) (int, error) {
    // implement batch writing using p.inserter.Put(ctx, rows)
    return 0, nil
}

func (p *StatItemUpdatedPlugin) Close() error {
    if p.client != nil {
        return p.client.Close()
    }
    return nil
}

func (p *StatItemUpdatedPlugin) HealthCheck(ctx context.Context) error {
    _, err := p.client.Dataset(p.cfg.DatasetID).Metadata(ctx)
    if err != nil {
        return fmt.Errorf("bigquery health check failed: %w", err)
    }
    return nil
}
```

Create one file per event type you handle, following the same structure.

### Step 3 — Add configuration

Add BigQuery configuration to `pkg/config/config.go`:

```go
type StorageConfig struct {
    Postgres PostgresConfig `envPrefix:"POSTGRES_"`
    S3       S3Config       `envPrefix:"S3_"`
    Kafka    KafkaConfig    `envPrefix:"KAFKA_"`
    MongoDB  MongoDBConfig  `envPrefix:"MONGODB_"`
    BigQuery BigQueryConfig `envPrefix:"BIGQUERY_"`  // ADD THIS
    Plugins  string         `env:"PLUGINS" envDefault:""`
}

type BigQueryConfig struct {
    ProjectID string `env:"PROJECT_ID"`
    DatasetID string `env:"DATASET_ID"`
}
```

### Step 4 — Wire into bootstrap layer

Add the `bigquery` case to the plugin switch in `internal/bootstrap/plugins.go`:

```go
import (
    // ... existing imports ...
    "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage/plugins/bigquery"
)

case "bigquery":
    siuPlugin = bigquery.NewStatItemUpdatedPlugin(bigquery.StatItemUpdatedPluginConfig{
        ProjectID: appCfg.Storage.BigQuery.ProjectID,
        DatasetID: appCfg.Storage.BigQuery.DatasetID,
        TableID:   "stat_item_updated_events",
    })
    // add one line per event type you handle
```

### Step 5 — Enable the plugin

```bash
export STORAGE_ENABLED_PLUGINS=bigquery
# or combine with existing: STORAGE_ENABLED_PLUGINS=postgres,bigquery
```

---

## Quick Reference

### Files to Modify When Handling a New AGS Event Type

1. **`pkg/proto/accelbyte-asyncapi/<service>/`** — Copy proto from AGS catalogue and regenerate
2. **`pkg/events/<type>.go`** — Event wrapper with `DeduplicationKey()` and `ToDocument()`
3. **`pkg/service/<type>.go`** — gRPC handler implementing the generated `OnMessage` interface
4. **`pkg/storage/plugins/<backend>/<type>.go`** — One file per backend
5. **`internal/bootstrap/plugins.go`** — Add to `StoragePlugins` struct and initialization
6. **`internal/bootstrap/processor.go`** — Add processor for the new type
7. **`internal/bootstrap/deduplicator.go`** — Add deduplicator for the new type
8. **`internal/app/app.go`** — Register the new gRPC service

### Files to Modify When Adding a New Storage Backend

1. **`pkg/storage/plugins/<backend>/<type>.go`** — Create plugin files (one per event type)
2. **`pkg/config/config.go`** — Add config struct for the backend
3. **`internal/bootstrap/plugins.go`** — Add case in the plugin switch

### Testing Your Plugin

```bash
# Set environment variables
export STORAGE_ENABLED_PLUGINS=<your-backend>
export STORAGE_<BACKEND>_<CONFIG_KEY>=<value>

# Run the service
go run main.go

# Check logs for plugin initialization
# Look for: "plugin initialized" log entries with your plugin name
```

---
