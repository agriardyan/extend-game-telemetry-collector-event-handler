# Telemetry Type Architecture Assessment

## Problem Statement

The current codebase uses a single `storage.TelemetryEvent` struct as a universal envelope for
all three telemetry categories:

```go
type TelemetryEvent struct {
    Kind            Kind   // runtime discriminator
    Namespace       string
    UserID          string
    ServerTimestamp int64
    SourceIP        string

    // Only one is non-nil at runtime — enforced by convention, not the compiler
    UserBehavior *pb.CreateUserBehaviorTelemetryRequest
    Gameplay     *pb.CreateGameplayTelemetryRequest
    Performance  *pb.CreatePerformanceTelemetryRequest
}
```

This is a **discriminated union** (tagged union). It works, but it has structural consequences:

1. **Non-exhaustive at compile time.** Every `switch e.Kind` in every plugin must be
   kept in sync with the set of `Kind` constants. The compiler cannot enforce this. A
   new kind added without a corresponding case is a silent no-op at best, a panic at worst.

2. **Viral modification.** Adding a new telemetry type requires touching:
   - `service.proto` (new messages + RPC)
   - `pkg/storage/event.go` (`Kind` const, new field, `ToDocument()`, `DeduplicationKey()`)
   - Every plugin's transform / `WriteBatch` switch
   - The gRPC service handler
   - Tests in multiple packages

3. **Illusory extensibility.** The plugin interface is generic (`StoragePlugin[T any]`), but
   `T` is pinned to `*TelemetryEvent` everywhere, so the generics provide no practical
   protection against the union-expansion problem.

---

## Current Architecture (Reference)

```
gRPC handler → wraps pb.Request into TelemetryEvent{Kind: X, ...}
             → Processor[*TelemetryEvent].Submit()
             → Batcher → fan-out → plugin.WriteBatch([]*TelemetryEvent)
                                     → switch event.Kind { ... }
```

Infrastructure that is already generic and reusable:

| Component | Type parameter |
|-----------|----------------|
| `Processor[T]` | `T storage.Deduplicatable` |
| `Batcher[T]` | `T any` |
| `StoragePlugin[T]` | `T any` |
| `Deduplicator[T]` | `T storage.Deduplicatable` |

This infrastructure could serve three separate type-parameterized pipelines with zero changes.

---

## Approach A — Three Implementations per Storage Backend

Keep `StoragePlugin[T]` as-is. For each backend, write three independent plugin structs, one
per event category. Eliminate `TelemetryEvent`; introduce three typed event wrappers that
carry server-added metadata alongside the protobuf payload.

```go
// pkg/storage/events/user_behavior.go
type UserBehaviorEvent struct {
    Namespace       string
    UserID          string
    ServerTimestamp int64
    SourceIP        string
    *pb.CreateUserBehaviorTelemetryRequest
}
func (e *UserBehaviorEvent) DeduplicationKey() string { ... }

// pkg/storage/events/gameplay.go
type GameplayEvent struct { ... }

// pkg/storage/events/performance.go
type PerformanceEvent struct { ... }
```

Three independent pipelines in main.go:

```go
userBehaviorProcessor := processor.NewProcessor[*events.UserBehaviorEvent](...)
gameplayProcessor     := processor.NewProcessor[*events.GameplayEvent](...)
performanceProcessor  := processor.NewProcessor[*events.PerformanceEvent](...)
```

Each gRPC handler submits to its own processor:

```go
func (s *TelemetryService) CreateGameplayTelemetry(ctx, req) {
    event := &events.GameplayEvent{ ... }
    s.gameplayProcessor.Submit(event)
}
```

Plugin directory structure:

```
pkg/storage/plugins/
  postgres/
    user_behavior.go   // PostgresUserBehaviorPlugin
    gameplay.go        // PostgresGameplayPlugin
    performance.go     // PostgresPerformancePlugin
  s3/
    user_behavior.go
    gameplay.go
    performance.go
  kafka/ ...
  mongodb/ ...
  noop/
    noop.go            // NoopPlugin[T any] — stays generic, needs no changes
```

### Pros

- **Zero switch statements in plugins.** `WriteBatch(ctx, []*GameplayEvent)` is typed at the
  call site. The compiler enforces correctness.
- **Open/Closed for new types.** Adding a fourth telemetry type requires only new files. No
  existing plugin code is modified.
- **Simple per-plugin logic.** Each plugin struct handles exactly one schema. `toRow()`,
  `toDocument()`, `toMessage()` are unambiguous.
- **Reuses all existing generic infrastructure** (`Processor`, `Batcher`, `Deduplicator`)
  without modification.
- **Per-type configuration.** Each pipeline can have independent batch size, flush interval,
  and deduplication policy.
- **Independently testable.** Unit tests for `GameplayPlugin` only need `GameplayEvent`
  fixtures. No union construction noise.

### Cons

- **Struct count.** 4 backends × 3 types = 12 concrete plugin structs (+ noop which stays
  generic). Significant boilerplate.
- **Connection / client duplication.** A Postgres plugin for user-behavior and a Postgres
  plugin for gameplay each open their own `*sql.DB`. This can be mitigated with shared
  connection pool composition, but it adds an internal wiring layer.
- **Main.go grows.** Three processors, three plugin slices, three deduplicators.
- **Config surface expands.** Per-type batch size, flush interval, etc. means more env vars.

---

## Approach B — Multiple Storage Interfaces per Plugin

Define a separate storage interface per telemetry type. A single plugin struct can implement
one, two, or all three, sharing internal state (DB connection, S3 client).

```go
// pkg/storage/plugin.go
type UserBehaviorPlugin interface {
    Name() string
    Initialize(ctx context.Context) error
    Filter(*events.UserBehaviorEvent) bool
    WriteBatch(ctx context.Context, events []*events.UserBehaviorEvent) (int, error)
    Close() error
    HealthCheck(ctx context.Context) error
}

type GameplayPlugin interface { /* same shape, different T */ }
type PerformancePlugin interface { /* same shape, different T */ }
```

One Postgres struct implements all three:

```go
// pkg/storage/plugins/postgres/postgres.go
type PostgresPlugin struct { db *sql.DB; ... }

func (p *PostgresPlugin) WriteBatch(ctx, []*events.UserBehaviorEvent) (int, error) { ... }
func (p *PostgresPlugin) WriteBatch(ctx, []*events.GameplayEvent) (int, error) { ... }
// ↑ Go does not allow this — same method name, different signature is not permitted
```

Go does not support method overloading. The above is **illegal**. Approach B as literally
stated is not implementable with a single struct and Go's interface system.

**Workaround A — distinct method names:**

```go
type TelemetryPlugin interface {
    WriteUserBehaviorBatch(ctx, []*events.UserBehaviorEvent) (int, error)
    WriteGameplayBatch(ctx, []*events.GameplayEvent) (int, error)
    WritePerformanceBatch(ctx, []*events.PerformanceEvent) (int, error)
}
```

This consolidates everything into one interface, but now every plugin **must** implement all
three methods, even if it only cares about one type. It also abandons the clean generic
`StoragePlugin[T]` abstraction.

**Workaround B — capability interfaces + type assertions:**

```go
type UserBehaviorCapable interface {
    WriteUserBehaviorBatch(ctx, []*events.UserBehaviorEvent) (int, error)
}
```

The router checks `if p, ok := plugin.(UserBehaviorCapable); ok { p.WriteUserBehaviorBatch(...) }`.
This is duck-typing via reflection and loses compile-time safety — the exact property we are
trying to gain.

### Pros

- Connection sharing is natural (one struct, one pool).
- One plugin registration per backend.
- Fewer total plugin structs.

### Cons

- **Fundamentally incompatible with Go's type system** in the most natural form.
- Every workaround either forces all plugins to handle all types, or introduces runtime type
  assertions — both defeat the goal.
- The existing generic `StoragePlugin[T]` infrastructure must be abandoned or heavily
  duplicated.
- Routing layer becomes non-trivial: a single plugin that "knows" about all three types still
  needs a dispatcher to route events to the right method.

**Verdict: Not recommended.** The approach is conceptually appealing but clashes with Go's
interface model in ways that produce worse outcomes than either of the other approaches.

---

## Alternative C — Typed Event Wrappers + Typed Processors (Recommended)

This is Approach A done rigorously with the existing generic machinery. It is worth naming
separately because the implementation guideline is more specific.

**Step 1 — Replace `TelemetryEvent` with three typed wrappers.**

Each wrapper carries the server-enriched metadata alongside the raw protobuf payload:

```go
// pkg/events/user_behavior.go
package events

type UserBehaviorEvent struct {
    Namespace       string
    UserID          string
    ServerTimestamp int64
    SourceIP        string
    Payload         *pb.CreateUserBehaviorTelemetryRequest
}

func (e *UserBehaviorEvent) DeduplicationKey() string {
    sessionID := ""
    if e.Payload.Context != nil {
        sessionID = e.Payload.Context.SessionId
    }
    return fmt.Sprintf("user_behavior:%s:%s:%s:%s",
        e.Namespace, e.UserID, e.Payload.EventId, sessionID)
}
```

**Step 2 — Three independent processor pipelines.**

```go
// main.go
userBehaviorProc := processor.NewProcessor[*events.UserBehaviorEvent](
    processorCfg, userBehaviorPlugins, userBehaviorDedup, logger)

gameplayProc := processor.NewProcessor[*events.GameplayEvent](
    processorCfg, gameplayPlugins, gameplayDedup, logger)

performanceProc := processor.NewProcessor[*events.PerformanceEvent](
    processorCfg, performancePlugins, performanceDedup, logger)
```

**Step 3 — Plugins are typed to their event.**

```go
// pkg/storage/plugins/postgres/gameplay.go
type GameplayPlugin struct { db *sql.DB; table string }

func (p *GameplayPlugin) WriteBatch(ctx context.Context, events []*events.GameplayEvent) (int, error) {
    // No switch needed. events[i].Payload is *pb.CreateGameplayTelemetryRequest — always.
}
```

**Step 4 — Connection sharing via composition.**

```go
// pkg/storage/plugins/postgres/shared.go
type postgresDB struct { db *sql.DB }

// pkg/storage/plugins/postgres/gameplay.go
type GameplayPlugin struct {
    *postgresDB
    table string
}

// pkg/storage/plugins/postgres/user_behavior.go
type UserBehaviorPlugin struct {
    *postgresDB
    table string
}
```

Both share the same `*postgresDB` instance, eliminating duplicate connection pools.

**Adding a fourth telemetry type in the future:**

1. Add proto messages + RPC → `make proto`
2. Create `pkg/events/combat.go` (new wrapper + `DeduplicationKey()`)
3. Create plugin files in each backend (new files only, no edits to existing ones)
4. Create `combatProc` in `main.go`
5. Add a `CreateCombatTelemetry` handler

Zero existing files modified outside of `main.go` and the new handler.

---

## Alternative D — Retain Union Type with Targeted Improvements

Keep `TelemetryEvent` as-is but add tooling to reduce the risk of incomplete switch handling:

- **Exhaustive switch via linter.** Use `exhaustive` or `exhaustruct` linter to flag
  unhandled cases at CI time.
- **`Must*` accessor pattern.** Add `MustUserBehavior()` helpers that panic on wrong kind
  rather than silently returning nil.
- **Interface-based dispatch table.** Register kind-specific handlers in a `map[Kind]HandlerFunc`
  so new kinds require a single registration point rather than a scattered set of case additions.

### Pros

- Zero architectural change, low risk.
- Minimal diff from current state.

### Cons

- Still one struct for all types — plugins are structurally coupled to every future type.
- Linter enforcement is a weaker guarantee than compile-time enforcement.
- Does not actually solve the modification scope problem; it only makes it slightly safer.
- Not suitable as a reference codebase showing extensible patterns.

---

## Comparison Matrix

| Criterion | Current (Union) | A / C: Typed Processors | B: Multi-Interface | D: Improved Union |
|-----------|:--------------:|:-----------------------:|:------------------:|:-----------------:|
| Compile-time type safety | ❌ | ✅ | ❌ (workarounds) | ❌ |
| New type requires modifying existing plugins | ✅ yes | ❌ no | ❌ no | ✅ yes |
| Switch statements in plugins | ✅ required | ❌ none | ❌ none | ✅ required |
| Reuses existing generics | ✅ | ✅ | ❌ | ✅ |
| Go type system compatibility | ✅ | ✅ | ⚠️ friction | ✅ |
| Connection sharing within backend | natural | via composition | natural | natural |
| Plugin struct count (4 backends) | 4 | 12 + shared | 4 | 4 |
| Main.go wiring complexity | low | medium | high | low |
| Per-type routing / config | ❌ | ✅ | ✅ | ❌ |
| Clarity as reference code | medium | **high** | low | medium |

---

## Recommendation

**Adopt Alternative C (Typed Event Wrappers + Typed Processors).**

The rationale is grounded in the stated priorities:

1. **Clarity over DRY.** Twelve plugin structs that each do one thing unambiguously are
   clearer than four plugin structs that each switch on a runtime discriminator. A developer
   reading `GameplayPlugin.WriteBatch` knows exactly what data they are working with.

2. **The generic infrastructure already supports this at no cost.** `Processor[T]`,
   `Batcher[T]`, `StoragePlugin[T]`, and `Deduplicator[T]` all work correctly with typed
   event wrappers. No framework changes required.

3. **True extensibility.** The Open/Closed Principle is satisfied in the most literal sense:
   adding a new telemetry category is entirely additive. Existing plugin files are not
   touched.

4. **Connection sharing solves the duplication concern.** The shared internal connection type
   (`*postgresDB`, S3 `*client`, etc.) eliminates duplicate pools without compromising per-type
   plugin independence.

5. **Approach B is architecturally attractive but Go-hostile.** The language's interface
   system does not support the pattern without workarounds that negate its benefits.

### Migration Path

The migration from the current union type to typed processors can be staged without a big bang:

| Phase | Action | Risk |
|-------|--------|------|
| 1 | Create `pkg/events/` with three typed wrappers, keep `TelemetryEvent` | None — additive |
| 2 | Migrate one backend (e.g., noop or postgres) to typed plugins | Isolated |
| 3 | Wire one typed processor in parallel with the existing one | Toggleable |
| 4 | Migrate remaining backends, decommission `TelemetryEvent` | Final |

Each phase is independently buildable and testable.
