# Telemetry Ingestion Service

High-throughput, distributed REST API in Go designed for streaming telemetry ingestion (e.g., IoT, V2X connected vehicles, and distributed systems). The service achieves high write scalability by decoupling HTTP request handling from disk persistence through in-memory buffered worker pools and PostgreSQL bulk streaming via the native binary `COPY` protocol.

---

## Architecture Overview

```
                      ┌───────────────────────────────────────────────┐
                      │              Telemetry Service                │
                      │                                               │
Incoming Request ───► │  [Rate Limiter (Redis / Sliding Window)]       │
                      │                       │                       │
                      │  [Authentication (ECDSA / Worker Pool)]       │
                      │                       │                       │
                      │  [HTTP Handler (chi / validator)]             │
                      │        │ (Non-blocking Enqueue)               │
                      │        ▼                                      │
                      │  [Buffered Channel Buffer (batchSize * 2)]    │
                      │        │                                      │
                      │        ▼                                      │
                      │  [Batch Ingester Consumer]                    │
                      │        │ (Flush on BatchSize or Ticker)       │
                      │        ▼                                      │
                      │  [pgxpool CopyFrom (Binary Bulk Insert)]      │
                      └───────────────────────┬───────────────────────┘
                                              │
                                              ▼
                                    [(PostgreSQL Database)]
```

### Ingestion Flow
1. **HTTP Ingestion**: Clients dispatch JSON payloads to `POST /api/v1/telemetry`.
2. **Rate Limiting & Defense**: Incoming requests are metered using a distributed sliding-window counter backed by Redis and atomic Lua scripts.
3. **Payload Validation**: Inputs are validated against schema boundaries using `go-playground/validator/v10`.
4. **Non-Blocking Hand-off**: Validated records are pushed immediately into an in-memory buffered channel (`batchSize * 2`). The handler returns `201 Created` immediately without waiting for database I/O.
5. **Batch Processing**: A background consumer groups logs into memory buffers and flushes them when either:
   - The buffer reaches `BATCH_SIZE` (e.g. 500 items).
   - The flush interval (`FLUSH_TIMEOUT_SEC`, default 3s) elapses.
6. **Optimized Bulk Persistence**: Batches are inserted into PostgreSQL using `pgxpool.CopyFrom`, utilizing PostgreSQL's native streaming `COPY` protocol rather than individual `INSERT` statements.
7. **Graceful Shutdown**: On `SIGINT`/`SIGTERM`, the HTTP server stops accepting new traffic, drains active requests, and signals the worker to flush remaining buffered items before exiting.

---

## Core Features & Modules

- **Non-Blocking Ingestion Pipeline** ([internal/handler/telemetry.go](file:///c:/Dev/Golang/project/internal/handler/telemetry.go), [internal/worker/ingester.go](file:///c:/Dev/Golang/project/internal/worker/ingester.go)): Decouples network I/O from storage latency using buffered channels and timed flush intervals.
- **PostgreSQL Repository with pgx v5** ([internal/repository/postgres.go](file:///c:/Dev/Golang/project/internal/repository/postgres.go)): High-performance persistence utilizing `pgxpool.Pool` and binary `CopyFrom` streaming for JSONB telemetry payloads.
- **Sliding-Window Rate Limiter** ([internal/middleware/ratelimit.go](file:///c:/Dev/Golang/project/internal/middleware/ratelimit.go)): Distributed rate limiting powered by Redis Sorted Sets (ZSET) and atomic Lua scripts, supporting client IP detection via `X-Forwarded-For` and a fail-open resiliency strategy.
- **V2X Cryptographic Authenticator** ([internal/auth/auth.go](file:///c:/Dev/Golang/project/internal/auth/auth.go)): Concurrent worker pool verifying ECDSA P-256 / SHA-256 signatures for vehicle-to-everything broadcasts with atomic spoof counters.
- **Production-Ready Logging**: Structured JSON logging via Go standard `log/slog`.

---

## API Endpoints

### 1. Ingest Telemetry Log
- **URL**: `/api/v1/telemetry`
- **Method**: `POST`
- **Content-Type**: `application/json`

**Request Body:**
```json
{
  "device_id": "sensor-device-alpha-12",
  "event_type": "temperature_reading",
  "payload": {
    "temperature": 23.4,
    "humidity": 45.2,
    "battery_pct": 98
  }
}
```

**Response (`201 Created`):**
```json
{
  "status": "success"
}
```

**Validation Constraints:**
- `device_id`: Required, maximum 255 characters.
- `event_type`: Required, maximum 100 characters.
- `payload`: Required arbitrary JSON object.

### 2. Health Check
- **URL**: `/health`
- **Method**: `GET`
- **Response (`200 OK`)**: `OK`

---

## Configuration

The service is configured via environment variables (with sensible defaults in [internal/config/config.go](file:///c:/Dev/Golang/project/internal/config/config.go)):

| Environment Variable | Default Value | Description |
|----------------------|---------------|-------------|
| `PORT` | `8080` | HTTP port the server listens on |
| `DATABASE_URL` | `postgres://user:password@localhost:5432/telemetry` | PostgreSQL connection pool string |
| `BATCH_SIZE` | `500` | Number of events buffered before triggering a batch database flush |
| `FLUSH_TIMEOUT_SEC` | `3` | Maximum seconds to wait before flushing incomplete batches |

---

## Database Schema

Defined in [scripts/init.sql](file:///c:/Dev/Golang/project/scripts/init.sql):

```sql
CREATE TABLE IF NOT EXISTS telemetry_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id VARCHAR(255) NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_telemetry_payload ON telemetry_logs USING GIN (payload);
CREATE INDEX idx_telemetry_device ON telemetry_logs (device_id);
```

---

## Getting Started

### Prerequisites
- [Go 1.22+](https://go.dev/) (Project uses Go 1.25 toolchain)
- [Docker](https://www.docker.com/) & Docker Compose
- (Optional) [Taskfile](https://taskfile.dev/) runner

### 1. Start Dependencies
Start PostgreSQL with the preconfigured initialization script:
```bash
docker compose up -d
```

### 2. Run the Service
Using [Taskfile.yml](file:///c:/Dev/Golang/project/Taskfile.yml):
```bash
task run
```
Or directly with the Go CLI:
```bash
go run cmd/server/main.go
```

### 3. Send a Test Event
```bash
curl -X POST http://localhost:8080/api/v1/telemetry \
  -H "Content-Type: application/json" \
  -d '{
    "device_id": "device-001",
    "event_type": "heartbeat",
    "payload": {"status": "ok", "uptime_sec": 3600}
  }'
```

---

## Available Commands

Using `task` (or running the underlying `cmds` directly):

| Task Command | Equivalent Go / Docker Command | Description |
|--------------|--------------------------------|-------------|
| `task run` | `go run cmd/server/main.go` | Run the service locally |
| `task build` | `go build -o bin/server.exe cmd/server/main.go` | Build the binary into `bin/` |
| `task test` | `go test ./...` | Execute unit and integration tests |
| `task bench` | `go test -bench=. -benchmem ./internal/...` | Run micro-benchmarks with memory allocations |
| `task lint` | `golangci-lint run ./...` | Run lint checks |
| `task docker` | `docker build -t telemetry-api:latest .` | Build minimal multi-stage scratch Docker image |

See [PERFORMANCE.md](file:///c:/Dev/Golang/project/PERFORMANCE.md) for the live benchmark evolution scorecard and memory profiling metrics.

---

## Project Structure

```
├── cmd/
│   └── server/
│       └── main.go          # Application entrypoint & dependency wiring
├── internal/
│   ├── auth/                # Concurrent ECDSA cryptographic verification worker pool
│   ├── config/              # Environment variable loading & defaults
│   ├── handler/             # HTTP routing (chi) and REST endpoint handlers
│   ├── middleware/          # Sliding-window Redis rate limiter middleware
│   ├── model/               # Data structures & JSON validation tags
│   ├── repository/          # PostgreSQL data access layer (pgxpool & CopyFrom)
│   └── worker/              # In-memory batch ingester & audit stream sanitizers
├── scripts/
│   └── init.sql             # PostgreSQL schema & GIN indexes
├── test/
│   └── integration/         # Integration test suite for API and repository
├── Dockerfile               # Multi-stage build producing a minimal scratch image
├── docker-compose.yml       # Local PostgreSQL service definition
├── Taskfile.yml             # Task automation configuration
└── PERFORMANCE.md           # Live benchmark scorecard & memory metrics
```
