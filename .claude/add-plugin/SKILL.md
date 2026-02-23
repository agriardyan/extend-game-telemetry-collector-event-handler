---
name: add-plugin
description: Scaffold a new storage plugin for the telemetry collector. Use when the user wants to add a new telemetry event type or a new storage backend.
disable-model-invocation: true
argument-hint: [type <name> | backend <name>]
allowed-tools: Read, Write, Edit, Glob, Grep, Bash
---

# add-plugin

Scaffolds plugin code for this telemetry collector project.

## Usage

```
/add-plugin type <name>      # New telemetry category across all existing backends
/add-plugin backend <name>   # New storage backend for all existing event types
```

**Arguments received:** $ARGUMENTS

Parse the arguments:
- First word (`type` or `backend`) — determines the mode
- Second word — the name of event (e.g., `economy`, `big_query`)

If arguments are missing or ambiguous, ask for clarification before proceeding.

---

## Before doing anything

Inject the current project state to avoid guessing:

Existing event types:
```
!`ls pkg/events/*.go | xargs -I{} basename {} .go | grep -v _test`
```

Existing backends:
```
!`ls pkg/storage/plugins/`
```

Plugin switch cases currently in main.go:
```
!`grep -n 'case "' main.go`
```

Read [CONVENTIONS.md](CONVENTIONS.md) before writing any code. It has the naming rules and
file patterns this project enforces.

---

## Mode: type

> **When**: first argument is `type`
>
> **Example**: `/add-plugin type economy` — adds economy telemetry support across all backends

### Step 1 — Read reference files

Read these **four files in full** before writing anything:

```
pkg/events/gameplay.go
pkg/storage/plugins/postgres/gameplay.go
pkg/storage/plugins/mongodb/gameplay.go
pkg/storage/plugins/s3/gameplay.go
pkg/storage/plugins/kafka/gameplay.go
```

These are your source of truth. Every file you create must follow the exact same structure,
substituting only the type name.

### Step 2 — Create the event wrapper

File: `pkg/events/<name>.go`

Copy the structure of `pkg/events/gameplay.go` exactly. Change:

| Original | Replace with |
|----------|--------------|
| `GameplayEvent` | `<Name>Event` (PascalCase) |
| `*pb.CreateGameplayTelemetryRequest` | `*pb.Create<Name>TelemetryRequest` |
| `"gameplay:"` prefix in `DeduplicationKey()` | `"<name>:"` |
| `"gameplay"` in `ToDocument()["kind"]` | `"<name>"` |

> **Note**: The proto type `*pb.Create<Name>TelemetryRequest` may not exist yet if the
> user has not added the proto definition. If it doesn't exist, use a `TODO` comment
> and placeholder comment explaining where to fill it in. Tell the user to define the proto
> first, then update the wrapper.

### Step 3 — Create backend plugin files

For each existing backend, create one plugin file. Use the gameplay plugin as template:

| Create | Based on |
|--------|----------|
| `pkg/storage/plugins/postgres/<name>.go` | `pkg/storage/plugins/postgres/gameplay.go` |
| `pkg/storage/plugins/mongodb/<name>.go` | `pkg/storage/plugins/mongodb/gameplay.go` |
| `pkg/storage/plugins/s3/<name>.go` | `pkg/storage/plugins/s3/gameplay.go` |
| `pkg/storage/plugins/kafka/<name>.go` | `pkg/storage/plugins/kafka/gameplay.go` |

Substitutions for every file:

| Original | Replace with |
|----------|--------------|
| `GameplayPlugin` | `<Name>Plugin` |
| `GameplayPluginConfig` | `<Name>PluginConfig` |
| `NewGameplayPlugin` | `New<Name>Plugin` |
| `"postgres:gameplay"` / `"s3:gameplay"` / etc. | `"<backend>:<name>"` |
| `"gameplay_events"` (default table/collection) | `"<name>_events"` |
| `*events.GameplayEvent` | `*events.<Name>Event` |
| `kind: "gameplay"` in Kafka headers | `kind: "<name>"` |

The S3 key path (`gameplay/<namespace>/year=...`) must also be updated to `<name>/<namespace>/...`.

For Kafka: the `compressionCodec` function already exists in `user_behavior.go` in the
kafka package. Do **not** add it again to the new file — just call it directly.

### Step 4 — Update main.go

Make **surgical edits** — do not rewrite main.go. Add the new type in three places:

**A. Plugin slice declaration** (near the other three declarations):
```go
var <name>Plugins []storage.StoragePlugin[*events.<Name>Event]
```

**B. Plugin switch** (inside each backend `case`, add a line for the new plugin):
```go
// Inside each existing case block, add:
<name>Plugin := <pkg>.New<Name>Plugin(<pkg>.<Name>PluginConfig{
    // same fields as the corresponding gameplay config in that case
})
```

Then initialize and append it alongside the other three:
```go
if err := <name>Plugin.Initialize(ctx); err != nil { ... }
<name>Plugins = append(<name>Plugins, <name>Plugin)
```

**C. Processor + deduplicator** (after the loop, alongside the existing three):
```go
<name>Dedup := buildDeduplicator[*events.<Name>Event](appCfg, logger)
<name>Proc  := processor.NewProcessor(processorCfg, <name>Plugins, <name>Dedup, logger)
<name>Proc.Start()
```

Also add shutdown in the graceful-shutdown block:
```go
if err := <name>Proc.Shutdown(shutdownTimeout); err != nil {
    logger.Error("<name> processor shutdown error", "error", err)
}
if err := <name>Dedup.Close(); err != nil {
    logger.Error("<name> deduplicator close error", "error", err)
}
for _, p := range <name>Plugins {
    if err := p.Close(); err != nil {
        logger.Error("failed to close plugin", "plugin", p.Name(), "error", err)
    }
}
```

### Step 5 — Build check

```bash
go build ./...
```

Report any errors and fix them before finishing.

---

## Mode: backend

> **When**: first argument is `backend`
>
> **Example**: `/add-plugin backend bigquery` — adds BigQuery support for all event types

### Step 1 — Read reference files

Read these files in full:

```
pkg/storage/plugins/postgres/user_behavior.go
pkg/storage/plugins/postgres/gameplay.go
pkg/storage/plugins/postgres/performance.go
main.go
pkg/config/config.go
```

### Step 2 — Gather requirements

Before writing code, ask the user (or infer from the arguments if already stated):

1. What Go import path is the client library? (e.g., `cloud.google.com/go/bigquery`)
2. Is it already in `go.mod`? If not, should you run `go get`?
3. What connection parameters are needed? (DSN, project+dataset, endpoint URL, credentials, etc.)
4. Does the backend auto-create tables/indexes, or must they exist beforehand?

If the user's initial message contains enough context, skip asking and state your assumptions.

### Step 3 — Add the dependency (if needed)

```bash
go get <library-import-path>
```

### Step 4 — Create the plugin package

Create `pkg/storage/plugins/<name>/` with three files — one per event type:

```
pkg/storage/plugins/<name>/user_behavior.go
pkg/storage/plugins/<name>/gameplay.go
pkg/storage/plugins/<name>/performance.go
```

For each file, implement all six interface methods. Follow the same structure as the Postgres
plugins (they are the closest to a generic template). Refer to [CONVENTIONS.md](CONVENTIONS.md)
for the DEVELOPER NOTE comment blocks that must appear on `Filter` and `transform`.

The `Name()` method must return `"<backend>:<type>"` (e.g., `"bigquery:gameplay"`).

Each plugin file is self-contained with its own config struct, its own client, and its own
`Initialize`/`Close` lifecycle.

### Step 5 — Add configuration

In `pkg/config/config.go`, add a new config struct and field to `StorageConfig`:

```go
type <Name>Config struct {
    // One field per connection parameter, with env tags following the existing pattern
}

type StorageConfig struct {
    // ... existing fields ...
    <Name> <Name>Config `envPrefix:"<NAME>_"`
}
```

### Step 6 — Update main.go

**A.** Add the import for the new package.

**B.** Add a new `case "<name>":` block in the plugin switch, creating all three event-type
plugins and assigning them to `ubPlugin`, `gpPlugin`, and `perfPlugin`.

**C.** Update the plugin-ready log to include the new backend name if useful.

### Step 7 — Build check

```bash
go build ./...
```

Report any errors and fix them before finishing.

---

## Finishing up

After all files are created and the build passes, summarize what was created:

- List every new file
- List every modified file with a one-line description of what changed
- Note any manual steps the user must take (e.g., proto definition, table creation,
  environment variables to set)
