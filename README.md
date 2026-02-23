# Extend Game Telemetry Collector Event Handler

A plugin-based [Extend Event Handler](https://docs.accelbyte.io/gaming-services/modules/foundations/extend/event-handler/) app for collecting, processing, and storing AGS events to one or more storage backends. It exposes a gRPC API to receive events published by AccelByte Gaming Services and routes them through a typed storage plugin system.

---

## Why This App?

When it comes to telemetry collection, one size does not fit all. Different games have different processing needs and different storage backends. Even within the same game, you might want to route different event types to different destinations — for example, sending stat item updates to S3 while streaming them to Kafka simultaneously.

We believe giving control to developers is the best way to enable creativity. This app is not a one-size-fits-all solution; it's a flexible framework that you can extend and customize to fit your game's unique telemetry needs. Whether you want to handle additional AGS event types, implement custom storage backends, or apply specific filtering and transformation logic, this app provides the foundation for you to build on.

## Flexibility by Design

| What you control | How |
|---|---|
| **AGS event types** | Pick any event from the [AGS event definitions](https://docs.accelbyte.io/gaming-services/knowledge-base/api-events/), add its proto to `pkg/proto/accelbyte-asyncapi/`, and generate the Go code |
| **Event filtering** | Implement `Filter()` per plugin — drop, sample, or gate events with plain Go |
| **Data transformation** | Implement your own transformation inside `WriteBatch()` — reshape, enrich, or redact before writing |
| **Storage destination** | Enable one or more backends: S3, PostgreSQL, Kafka, MongoDB, or a custom implementation |
| **Deduplication strategy** | In-memory TTL cache or Redis for distributed deployments |
| **Processing throughput** | Tune worker count, batch size, and flush interval per plugin |

Multiple storage plugins run in parallel. A single event stream can go to a data lake, a queryable database, and a Kafka topic at the same time, with each plugin applying its own filter and transformation independently.

---

## Architecture

```
┌───────────────────────────────────────────────────────────────┐
│               AccelByte Gaming Services (AGS)                 │
│           publishes events via the Kafka Connect              │
└────────────────────────────┬──────────────────────────────────┘
                             │  gRPC
                             ▼
┌───────────────────────────────────────────────────────────────┐
│  gRPC Event Handler Layer                                     │
│  - AccelByte IAM token validation                             │
│  - Event enrichment (server timestamp, etc.)                        │
│  - Non-blocking response  (<5 ms target latency)              │
└────────────────────────────┬──────────────────────────────────┘
                             │
                             ▼
┌───────────────────────────────────────────────────────────────┐
│  Deduplication Layer                                          │
│  - Memory (single-instance)  or  Redis (distributed)          │
│  - SHA-256 event fingerprinting with configurable TTL         │
└────────────────────────────┬──────────────────────────────────┘
                             │
                             ▼
┌───────────────────────────────────────────────────────────────┐
│  Async Processor (one per event type)                         │
│  - Buffered channel  (default 10 000 events)                  │
│  - Worker pool       (default 10 goroutines)                  │
│  - Smart batching    (size OR time trigger)                   │
│  - Graceful shutdown (flushes all in-flight events)           │
└────────────────────────────┬──────────────────────────────────┘
                             │  fan-out (parallel)
          ┌──────────────────┼──────────────────┬──────────────┐
          ▼                  ▼                  ▼              ▼
    ┌──────────┐      ┌──────────┐      ┌──────────┐    ┌──────────┐
    │   S3     │      │PostgreSQL│      │  Kafka   │    │ MongoDB  │
    │ Filter   │      │ Filter   │      │ Filter   │    │ Filter   │
    │ Transform│      │ Transform│      │ Transform│    │ Transform│
    │ Write    │      │ Write    │      │ Write    │    │ Write    │
    └──────────┘      └──────────┘      └──────────┘    └──────────┘
    Data lake         SQL analytics    Real-time stream  Flexible schema
```

---

## Batch Processing

The event handler returns immediately after pushing the event onto a buffered channel — it does not wait for any storage write. Actual writes happen asynchronously in a worker pool.

Each event type has its own processor that runs this pipeline:

```
incoming event
      │
      ▼
 buffered channel  ──── back-pressure: blocks if full
      │
      ▼
 worker goroutine  (configurable pool size)
      │
      ▼
 batcher           ──── flush when:  batch reaches PROCESSOR_BATCH_SIZE
      │                              OR PROCESSOR_FLUSH_INTERVAL elapses
      │                              OR shutdown signal received
      ▼
 fan-out to all enabled plugins (parallel goroutines)
      │
      ├─ plugin.Filter()     ← drop unwanted events per plugin
      └─ plugin.WriteBatch() ← transform + write the accepted events
```

Batching reduces the number of round-trips to each backend. For example, with `PROCESSOR_BATCH_SIZE=100` and `PROCESSOR_FLUSH_INTERVAL=5s`, at most 100 events are buffered before a write, and no event waits longer than 5 seconds even at low traffic.

On shutdown, each processor drains its remaining events before closing, so no in-flight data is lost.

---

## Handled AGS Event Types

Event handlers are implemented for the following AGS events. Each type has its own independent processor, deduplicator, and plugin slice.

| Service | Event | Proto Source |
|---|---|---|
| `StatisticStatItemUpdatedService` | Fired when a user's stat item value changes | [accelbyte-api-proto](https://github.com/AccelByte/accelbyte-api-proto/tree/main/asyncapi/accelbyte/social/statistic/v1/statistic.proto) |

A spike in one event type never delays processing of another.

---

## Built-in Storage Plugins

| Plugin | Use case |
|---|---|
| `s3` | Data lake, Athena/Glue/Spark queries |
| `postgres` | SQL analytics, JSONB queries |
| `kafka` | Real-time streaming, downstream consumers |
| `mongodb` | Flexible schema, rich document queries |
| `noop` | Testing, benchmarking, dry-runs |

Enable one or many simultaneously via environment variable:

```bash
STORAGE_ENABLED_PLUGINS=postgres,kafka,s3
```

---

## Quick Start

### Prerequisites

- Go 1.21+
- An AccelByte Gaming Services namespace with a configured OAuth client
- At least one storage backend reachable (or use `noop` for local testing)

### 1. Clone and configure

```bash
cp .env.example .env
# Fill in AB_BASE_URL, AB_CLIENT_ID, AB_CLIENT_SECRET, AB_NAMESPACE
```

### 2. Choose your storage backend

```bash
# Local test — no external storage needed
STORAGE_ENABLED_PLUGINS=noop

# PostgreSQL
STORAGE_ENABLED_PLUGINS=postgres
STORAGE_POSTGRES_DSN=postgres://user:pass@localhost:5432/telemetry?sslmode=disable

# Multiple backends simultaneously
STORAGE_ENABLED_PLUGINS=postgres,kafka,s3
STORAGE_KAFKA_BROKERS=localhost:9092
STORAGE_KAFKA_TOPIC=game-telemetry
STORAGE_S3_BUCKET=my-telemetry-bucket
STORAGE_S3_REGION=us-east-1
```

### 3. Run

```bash
go run main.go
```

The service starts two listeners:

| Port | Protocol | Purpose |
|---|---|---|
| `6565` | gRPC | AGS event handler API |
| `8080` | HTTP | Prometheus metrics (`/metrics`) |

---

## Configuration

Configuration is driven entirely by environment variables. See `.env.example` for a full reference, and `config/config.yaml` for default values and available knobs (processor workers, batch sizes, flush intervals, deduplication TTL, etc.).

Key variables:

| Variable | Default | Description |
|---|---|---|
| `STORAGE_ENABLED_PLUGINS` | `noop` | Comma-separated list of active plugins |
| `PROCESSOR_WORKERS` | `10` | Goroutines per processor |
| `PROCESSOR_BATCH_SIZE` | `100` | Events per batch flush |
| `PROCESSOR_FLUSH_INTERVAL` | `5s` | Max time before forced flush |
| `DEDUP_ENABLED` | `false` | Enable event deduplication |
| `DEDUP_TYPE` | `memory` | `memory` or `redis` |
| `DEDUP_TTL` | `1h` | Deduplication window |
| `PLUGIN_GRPC_SERVER_AUTH_ENABLED` | `true` | Toggle IAM authentication |

---

## Extending the App

The plugin system is the primary extension point. Two scenarios are fully documented:

1. **Handle a new AGS event type** — find the event's proto definition from the [AGS event catalogue](https://docs.accelbyte.io/gaming-services/knowledge-base/api-events/), add it to `pkg/proto/accelbyte-asyncapi/`, generate Go code, implement the event wrapper and storage plugins, then wire it through the bootstrap layer.

2. **Add a new storage backend** — implement the `StoragePlugin[T]` interface and register it, making your backend available to every event type with zero changes to the core.

See **[docs/PLUGIN_DEVELOPMENT.md](docs/PLUGIN_DEVELOPMENT.md)** for step-by-step instructions, complete code examples, and naming conventions.

### Plugin interface at a glance

```go
type StoragePlugin[T any] interface {
    Name()                                      string
    Initialize(ctx context.Context)             error
    Filter(event T)                             bool
    WriteBatch(ctx context.Context, events []T) (int, error)
    Close()                                     error
    HealthCheck(ctx context.Context)            error
}
```

Each plugin is self-contained — its own config struct, its own connection, its own filter and transformation logic. `Filter` decides which events reach `WriteBatch`; inside `WriteBatch` you implement whatever transformation the destination requires before writing. There is no shared state between plugins.

---

## Observability

- **Prometheus metrics** — gRPC server metrics via `go-grpc-prometheus`, plus processor queue depth and throughput, available at `:8080/metrics`
- **OpenTelemetry tracing** — gRPC spans with B3 and W3C TraceContext propagation
- **Structured logging** — `log/slog` with JSON output, configurable log level
- **Health check** — standard gRPC health protocol on port `6565`

---

## Project Structure

```
extend-game-telemetry-collector-event-handler/
├── main.go                          # Entry point
├── config/config.yaml               # Default configuration
│
├── internal/
│   ├── app/                         # Application wiring
│   └── bootstrap/                   # Plugin, processor, and deduplicator initialization
│
├── pkg/
│   ├── config/                      # Config loading from env vars + YAML
│   ├── events/                      # Typed event wrappers (one per AGS event type)
│   ├── dedup/                       # Deduplicator interface + memory/Redis/noop impls
│   ├── processor/                   # Generic async processor: worker pool + batcher
│   ├── service/                     # gRPC service handlers
│   ├── proto/accelbyte-asyncapi/    # AGS AsyncAPI proto definitions
│   └── storage/
│       ├── plugin.go                # StoragePlugin[T] interface
│       ├── deduplicatable.go        # Deduplicatable constraint
│       └── plugins/
│           ├── s3/                  # S3 plugin
│           ├── postgres/            # PostgreSQL plugin
│           ├── kafka/               # Kafka plugin
│           ├── mongodb/             # MongoDB plugin
│           └── noop/                # No-op plugin for testing
│
└── docs/
    └── PLUGIN_DEVELOPMENT.md        # Guide: add AGS event types and storage backends
```

---

## Technology Stack

| Concern | Library |
|---|---|
| API protocol | gRPC |
| Schema | Protocol Buffers v3 (AGS AsyncAPI definitions) |
| Authentication | AccelByte IAM SDK |
| S3 storage | AWS SDK v2 |
| PostgreSQL | `lib/pq` |
| Kafka | `kafka-go` |
| MongoDB | official `mongo-driver` |
| Deduplication cache | `go-redis/v9` |
| Metrics | Prometheus + `go-grpc-prometheus` |
| Tracing | OpenTelemetry + B3 propagation |
| Logging | `log/slog` (standard library) |

---

## License

See [LICENSE.txt](LICENSE.txt).
