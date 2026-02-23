# Plugin Conventions Reference

Loaded on demand by add-plugin/SKILL.md. Contains the naming rules, code patterns, and
structural conventions that all plugins in this project must follow.

---

## Naming conventions

| Item | Format | Example |
|------|--------|---------|
| Event type name (snake_case) | `<name>` | `economy` |
| Event type name (PascalCase) | `<Name>` | `Economy` |
| Event wrapper file | `pkg/events/<name>.go` | `pkg/events/economy.go` |
| Event wrapper struct | `<Name>Event` | `EconomyEvent` |
| Plugin config struct | `<Name>PluginConfig` | `EconomyPluginConfig` |
| Plugin struct | `<Name>Plugin` | `EconomyPlugin` |
| Constructor | `New<Name>Plugin` | `NewEconomyPlugin` |
| `Name()` return value | `"<backend>:<name>"` | `"postgres:economy"` |
| Default table (Postgres/MongoDB) | `"<name>_events"` | `"economy_events"` |
| Default collection (MongoDB) | `"<name>_events"` | `"economy_events"` |
| Kafka topic suffix | `".<name>"` appended to base topic | `"telemetry.economy"` |
| S3 key path segment | `<name>/` | `economy/` |
| Kafka `kind` header | `<name>` | `economy` |
| `kind` field in `ToDocument()` | `<name>` | `"economy"` |
| `DeduplicationKey()` prefix | `"<name>:"` | `"economy:"` |

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
func (p *EconomyPlugin) Filter(_ *events.EconomyEvent) bool { return true }
```

### On `transform` (database backends)

```go
// transform converts an EconomyEvent into a row map for Postgres insertion.
// ------------------------------------------------------------------------------
// DEVELOPER NOTE:
// Customize this method to reshape events before storage.
// For example, extract additional fields or apply data masking.
// ------------------------------------------------------------------------------
func (p *EconomyPlugin) transform(e *events.EconomyEvent) (map[string]interface{}, error) {
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

## internal/boostrap/plugins.go structure (reference)

The plugin loop in `internal/boostrap/plugins.go` follows this structure:

```
for _, pluginName := range appCfg.GetEnabledPlugins() {
    var (
        ubPlugin   StoragePlugin[*events.UserBehaviorEvent]
        gpPlugin   StoragePlugin[*events.GameplayEvent]
        perfPlugin StoragePlugin[*events.PerformanceEvent]
        // ADD: <name>Plugin StoragePlugin[*events.<Name>Event]
    )

    switch pluginName {
    case "postgres":
        ubPlugin   = postgres.NewUserBehaviorPlugin(...)
        gpPlugin   = postgres.NewGameplayPlugin(...)
        perfPlugin = postgres.NewPerformancePlugin(...)
        // ADD: <name>Plugin = postgres.New<Name>Plugin(...)
    // ... other cases ...
    }

    // initialize each plugin individually
    // append each plugin to its slice
}

// After the loop:
// - buildDeduplicator for each type
// - processor.NewProcessor for each type
// - processor.Start()
```

When adding a new **type**, add one line per backend case. When adding a new **backend**,
add one case block with lines for each existing type.
