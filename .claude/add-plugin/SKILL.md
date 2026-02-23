---
name: add-plugin
description: Scaffold a new storage plugin for the telemetry collector. Use when the user wants to handle a new AGS event type or add a new storage backend.
disable-model-invocation: true
argument-hint: [type <name> | backend <name>]
allowed-tools: Read, Write, Edit, Glob, Grep, Bash
---

# add-plugin

Scaffolds plugin code for this telemetry collector project.

## Usage

```
/add-plugin type <name>      # New AGS event type across all existing backends
/add-plugin backend <name>   # New storage backend for all existing event types
```

**Arguments received:** $ARGUMENTS

Parse the arguments:
- First word (`type` or `backend`) — determines the mode
- Second word — the name in snake_case (e.g., `stat_item_deleted`, `big_query`)

If arguments are missing or ambiguous, ask for clarification before proceeding.

> **This is an Extend Event Handler app.** Event schemas come from AccelByte Gaming Services.
> Developers must NOT define their own proto messages. All proto files come from the
> [AGS event catalogue](https://docs.accelbyte.io/gaming-services/knowledge-base/api-events/)
> (source: https://github.com/AccelByte/accelbyte-api-proto/tree/main/asyncapi).

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

Current bootstrap plugin cases:
```
!`grep -n 'case "' internal/bootstrap/plugins.go`
```

Read [CONVENTIONS.md](CONVENTIONS.md) before writing any code. It has the naming rules and
file patterns this project enforces.

---

## Mode: type

> **When**: first argument is `type`
>
> **Example**: `/add-plugin type stat_item_deleted` — adds stat_item_deleted event handling across all backends

### Step 1 — Verify the AGS proto

Check whether the proto for this event already exists under `pkg/proto/accelbyte-asyncapi/`:

```
!`find pkg/proto/accelbyte-asyncapi -name "*.proto" | head -20`
```

If the proto is not present, tell the user to copy it from
https://github.com/AccelByte/accelbyte-api-proto/tree/main/asyncapi
into the matching path under `pkg/proto/accelbyte-asyncapi/`, then run `./proto.sh` to
regenerate Go code. Do not proceed until the generated `*_grpc.pb.go` exists.

If the proto already exists, read the relevant `*_grpc.pb.go` to identify:
- The server interface name (e.g., `StatisticStatItemDeletedServiceServer`)
- The `OnMessage` method signature and its request message type (e.g., `*StatItemDeleted`)
- The `Unimplemented*` embed struct name

### Step 2 — Read reference files

Read these **files in full** before writing anything:

```
pkg/events/stat_item_updated.go
pkg/service/stat_item_updated.go
pkg/storage/plugins/postgres/stat_item_updated.go
pkg/storage/plugins/mongodb/stat_item_updated.go
pkg/storage/plugins/s3/stat_item_updated.go
pkg/storage/plugins/kafka/stat_item_updated.go
```

These are your source of truth. Every file you create must follow the exact same structure,
substituting only the type name and proto message fields.

Also read the bootstrap files to understand the current wiring:
```
internal/bootstrap/plugins.go
internal/bootstrap/processor.go
internal/bootstrap/deduplicator.go
internal/app/app.go
```

### Step 3 — Create the event wrapper

File: `pkg/events/<name>.go`

Copy the structure of `pkg/events/stat_item_updated.go`. The event wrapper must:

- Declare a struct with the event envelope fields (`ID`, `Version`, `Namespace`, `Name`,
  `UserID`, `SessionID`, `Timestamp`, `ServerTimestamp`) and a typed `Payload` field.
- If the AGS event's payload type is the same as an existing one (e.g., `StatItem`), reuse
  the existing struct rather than defining a duplicate.
- Implement `DeduplicationKey() string` — use fields that make the event unique in your domain.
- Implement `ToDocument() map[string]interface{}` — flat map for JSON/BSON serialization.

Key substitutions:

| Original | Replace with |
|----------|--------------|
| `StatItemUpdatedEvent` | `<Name>Event` (PascalCase) |
| `"stat_item_updated:"` prefix | `"<name>:"` |
| `"stat_item_updated"` in `ToDocument()["kind"]` | `"<name>"` |

### Step 4 — Create the gRPC service handler

File: `pkg/service/<name>.go`

Copy the structure of `pkg/service/stat_item_updated.go`. Change:

| Original | Replace with |
|----------|--------------|
| `StatItemUpdatedService` | `<Name>Service` |
| `UnimplementedStatisticStatItemUpdatedServiceServer` | Unimplemented struct from the generated grpc file |
| `*statistic.StatItemUpdated` | The correct proto message type for this event |
| `RegisterStatisticStatItemUpdatedServiceServer` | The correct Register function for this service |
| All field mappings in `OnMessage` | Fields from the actual proto message |

The handler must:
1. Validate `req != nil`
2. Use `req.Namespace` (fall back to `s.namespace` if empty)
3. Set `UserID` from `req.UserId`
4. Set `ServerTimestamp` to `time.Now().UnixMilli()`
5. Map proto fields into the event wrapper struct
6. Call `s.proc.Submit(event)` and return gRPC status errors on failure

### Step 5 — Create backend plugin files

For each existing backend, create one plugin file:

| Create | Based on |
|--------|----------|
| `pkg/storage/plugins/postgres/<name>.go` | `pkg/storage/plugins/postgres/stat_item_updated.go` |
| `pkg/storage/plugins/mongodb/<name>.go` | `pkg/storage/plugins/mongodb/stat_item_updated.go` |
| `pkg/storage/plugins/s3/<name>.go` | `pkg/storage/plugins/s3/stat_item_updated.go` |
| `pkg/storage/plugins/kafka/<name>.go` | `pkg/storage/plugins/kafka/stat_item_updated.go` |

Substitutions for every file:

| Original | Replace with |
|----------|--------------|
| `StatItemUpdatedPlugin` | `<Name>Plugin` |
| `StatItemUpdatedPluginConfig` | `<Name>PluginConfig` |
| `NewStatItemUpdatedPlugin` | `New<Name>Plugin` |
| `"postgres:stat_item_updated"` / etc. | `"<backend>:<name>"` |
| `"stat_item_updated_events"` | `"<name>_events"` |
| `*events.StatItemUpdatedEvent` | `*events.<Name>Event` |
| `kind: "stat_item_updated"` in Kafka headers | `kind: "<name>"` |
| `stat_item_updated/<namespace>/...` S3 key path | `<name>/<namespace>/...` |

For Kafka: `compressionCodec` is defined in `stat_item_updated.go` in the kafka package.
Do **not** add it again — just call it directly.

### Step 6 — Wire into bootstrap layer

Make **surgical edits** — do not rewrite any file.

**A. `internal/bootstrap/plugins.go`**

Add `<Name> []storage.StoragePlugin[*events.<Name>Event]` to `StoragePlugins`.

Add `<Name>: []storage.StoragePlugin[*events.<Name>Event]{}` to the struct literal.

Add `var <name>Plugin storage.StoragePlugin[*events.<Name>Event]` inside the loop's `var` block.

Add plugin construction inside each backend `case`, mirroring the `siuPlugin` pattern.

Add initialize, log, and append after the switch:
```go
if err := <name>Plugin.Initialize(ctx); err != nil {
    return nil, err
}
logger.Info("plugin initialized", "plugin", <name>Plugin.Name())
plugins.<Name> = append(plugins.<Name>, <name>Plugin)
```

Add noop fallback inside `if len(plugins.StatItemUpdated) == 0`.

Add `"<name>", len(plugins.<Name>)` to the final `logger.Info` call.

Add close loop in `CloseStoragePlugins`.

**B. `internal/bootstrap/processor.go`**

Add `<Name> *processor.Processor[*events.<Name>Event]` to `Processors`.

Add `<Name>: processor.NewProcessor(processorCfg, plugins.<Name>, dedups.<Name>, logger)` to the struct literal.

Add `procs.<Name>.Start()`.

Add shutdown call in `ShutdownProcessors`.

**C. `internal/bootstrap/deduplicator.go`**

Add `<Name> dedup.Deduplicator[*events.<Name>Event]` to `Deduplicators`.

Add `<Name>: buildDeduplicator[*events.<Name>Event](appCfg, logger)` to the struct literal.

Add close call in `CloseDeduplicators`.

**D. `internal/app/app.go`**

Register the service after the existing service registrations:
```go
<name>Svc := service.New<Name>Service(
    bootstrap.GetNamespace(),
    app.processors.<Name>,
    app.logger,
)
statistic.Register<ServiceName>Server(grpcServer, <name>Svc)
```

### Step 7 — Build check

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
pkg/storage/plugins/postgres/stat_item_updated.go
internal/bootstrap/plugins.go
pkg/config/config.go
```

Also discover all current event types:
```
!`ls pkg/events/*.go | xargs -I{} basename {} .go | grep -v _test`
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

Create `pkg/storage/plugins/<name>/` with one file per existing event type.

For each event type file, implement all six interface methods following the same structure as
the Postgres plugins. Refer to [CONVENTIONS.md](CONVENTIONS.md) for the required DEVELOPER
NOTE comment blocks on `Filter` and `transform`.

The `Name()` method must return `"<backend>:<type>"` (e.g., `"bigquery:stat_item_updated"`).

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

### Step 6 — Wire into bootstrap layer

In `internal/bootstrap/plugins.go`:

**A.** Add the import for the new package.

**B.** Add a new `case "<name>":` block in the plugin switch. For each existing event type,
create and assign its plugin variable (e.g., `siuPlugin`, `sidPlugin`, etc.).

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
- Note any manual steps the user must take (e.g., proto generation, table creation,
  environment variables to set)
