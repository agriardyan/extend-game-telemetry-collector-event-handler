# Plugin Conventions Reference

Loaded on demand by add-plugin/SKILL.md. Contains the naming rules, code patterns, and
structural conventions that all plugins in this project must follow.

---

## Context

This is an **Extend Event Handler** app. Event schemas are defined by AccelByte Gaming
Services. All proto files come from the
[AGS event catalogue](https://docs.accelbyte.io/gaming-services/knowledge-base/api-events/)
(source: https://github.com/AccelByte/accelbyte-api-proto/tree/main/asyncapi).
Developers must not define their own proto messages.

---

## Naming conventions

| Item | Format | Example |
|------|--------|---------|
| Event type name (snake_case) | `<name>` | `stat_item_deleted` |
| Event type name (PascalCase) | `<Name>` | `StatItemDeleted` |
| Event wrapper file | `pkg/events/<name>.go` | `pkg/events/stat_item_deleted.go` |
| Event wrapper struct | `<Name>Event` | `StatItemDeletedEvent` |
| Service handler file | `pkg/service/<name>.go` | `pkg/service/stat_item_deleted.go` |
| Service handler struct | `<Name>Service` | `StatItemDeletedService` |
| Plugin config struct | `<Name>PluginConfig` | `StatItemDeletedPluginConfig` |
| Plugin struct | `<Name>Plugin` | `StatItemDeletedPlugin` |
| Constructor | `New<Name>Plugin` | `NewStatItemDeletedPlugin` |
| `Name()` return value | `"<backend>:<name>"` | `"postgres:stat_item_deleted"` |
| Default table (Postgres/MongoDB) | `"<name>_events"` | `"stat_item_deleted_events"` |
| Default collection (MongoDB) | `"<name>_events"` | `"stat_item_deleted_events"` |
| Kafka topic suffix | `".<name>"` appended to base topic | `"telemetry.stat_item_deleted"` |
| S3 key path segment | `<name>/` | `stat_item_deleted/` |
| Kafka `kind` header | `<name>` | `stat_item_deleted` |
| `kind` field in `ToDocument()` | `<name>` | `"stat_item_deleted"` |
| `DeduplicationKey()` prefix | `"<name>:"` | `"stat_item_deleted:"` |

---

## Required comment blocks

Every plugin file must include these exact DEVELOPER NOTE blocks on `Filter` and `transform`.
Copy verbatim — only the method signature changes:

### On `Filter`

```go
// Filter determines if an event should be processed by this plugin.
// ------------------------------------------------------------------------------
// DEVELOPER NOTE:
// Implement custom filtering logic here. Return false to skip an event.
// For example, filter out events from certain namespaces or users.
// ------------------------------------------------------------------------------
func (p *StatItemDeletedPlugin) Filter(_ *events.StatItemDeletedEvent) bool { return true }
```

### On `transform` (database backends)

```go
// transform converts a StatItemDeletedEvent into a row map for Postgres insertion.
// ------------------------------------------------------------------------------
// DEVELOPER NOTE:
// Customize this method to reshape events before storage.
// For example, extract additional fields or apply data masking.
// ------------------------------------------------------------------------------
func (p *StatItemDeletedPlugin) transform(e *events.StatItemDeletedEvent) (map[string]interface{}, error) {
```

### On `WriteBatch` (S3 backend)

```go
// WriteBatch serializes the batch to JSON and uploads it to S3.
// ------------------------------------------------------------------------------
// DEVELOPER NOTE:
// Customize transform logic here to reshape or enrich events before upload.
// The S3 key format is: {prefix}/<name>/{namespace}/year=YYYY/month=MM/day=DD/{ts}.json
// ------------------------------------------------------------------------------
```

---

## File structure rules

- **Each plugin file is fully self-contained.** It has its own config struct, its own client
  handle, and its own `Initialize`/`Close` lifecycle. No shared state across sibling files.

- **No `shared.go` files.** If a helper is needed by all files in a backend package (e.g.,
  `compressionCodec` in the kafka package), it lives in one of the plugin files with a clear
  comment explaining its package-wide scope. It is NOT placed in a `shared.go` file.

- **The `transform` method is always unexported.** It is a private implementation detail
  called only by `WriteBatch`.

- **`WriteBatch` skips bad events, never aborts the batch.** On transform error: log at Warn
  level, continue. On write error: return 0 and the error.

---

## Error patterns

```go
// Initialize: wrap all errors with context
return fmt.Errorf("failed to connect to <backend>: %w", err)

// WriteBatch: skip per-event failures
p.logger.Warn("failed to transform event, skipping", "error", err, "user_id", e.UserID)

// WriteBatch: return write failures
return 0, fmt.Errorf("failed to write to <backend>: %w", err)
```

---

## Logging patterns

```go
// Initialize: set logger from default with plugin name attr
p.logger = slog.Default().With("plugin", p.Name())

// Initialize: log when ready
p.logger.Info("<backend> plugin initialized", "table", p.cfg.Table, ...)

// WriteBatch: log on success
p.logger.Info("batch written to <backend>", "table", p.cfg.Table, "count", n)

// Close: log before closing
p.logger.Info("<backend> plugin closing", "table", p.cfg.Table)
```

---

## Bootstrap layer structure (reference)

Plugins, processors, and deduplicators are wired in `internal/bootstrap/`, not `main.go`.

### `internal/bootstrap/plugins.go`

```
StoragePlugins struct {
    StatItemUpdated []StoragePlugin[*events.StatItemUpdatedEvent]
    // ADD: <Name> []StoragePlugin[*events.<Name>Event]
}

InitializeStoragePlugins:
    for _, pluginName := range appCfg.GetEnabledPlugins() {
        var (
            siuPlugin StoragePlugin[*events.StatItemUpdatedEvent]
            // ADD: <name>Plugin StoragePlugin[*events.<Name>Event]
        )
        switch pluginName {
        case "postgres":
            siuPlugin = postgres.NewStatItemUpdatedPlugin(...)
            // ADD: <name>Plugin = postgres.New<Name>Plugin(...)
        // ... other cases ...
        }
        // initialize each plugin individually
        // append each plugin to its slice
    }
    // noop fallback if no plugins configured
```

### `internal/bootstrap/processor.go`

```
Processors struct {
    StatItemUpdated *processor.Processor[*events.StatItemUpdatedEvent]
    // ADD: <Name> *processor.Processor[*events.<Name>Event]
}
```

### `internal/bootstrap/deduplicator.go`

```
Deduplicators struct {
    StatItemUpdated dedup.Deduplicator[*events.StatItemUpdatedEvent]
    // ADD: <Name> dedup.Deduplicator[*events.<Name>Event]
}
```

### `internal/app/app.go`

gRPC services are registered here. After adding the processor, register the service:
```go
statistic.Register<ServiceName>Server(grpcServer, <name>Svc)
```

When adding a new **event type**, add one line per backend case and update all four bootstrap
files. When adding a new **backend**, add one case block with a line for each existing event type.
