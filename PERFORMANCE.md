# Telemetry Service — Performance & Engineering Evolution Scorecard

This document serves as the empirical record of performance, memory efficiency, and resilience enhancements across the lifecycle of the Telemetry Ingestion Service. Every phase is verified with reproducible Go benchmarks, runtime measurements, and statistical comparisons (`benchstat`).

---

## Evolution Scorecard

| Metric | Phase 0 (Baseline) | Phase 1 (Backpressure) | Phase 2 (Worker Pool) | Phase 3 (pprof & Metrics) | Phase 4 (Zero-Alloc / sync.Pool) | Phase 5 (Macro Load Test) |
|---|---|---|---|---|---|---|
| **Handler Latency (Sequential)** | `5,320 ns/op` | *Pending* | *Pending* | *Pending* | *Target < 1,500 ns/op* | — |
| **Handler Latency (Parallel - 16T)** | `5,294 ns/op` | *Pending* | *Pending* | *Pending* | *Target < 1,200 ns/op* | — |
| **Memory Allocated (`B/op`)** | `7,477 B/op` | *Pending* | *Pending* | *Pending* | *Target < 1,000 B/op* | — |
| **Heap Allocations (`allocs/op`)** | `48 allocs/op` | *Pending* | *Pending* | *Pending* | *Target < 3 allocs/op* | — |
| **Ingester Channel Enqueue** | `164.7 ns/op` | *Pending* | *Pending* | *Pending* | *Pending* | — |
| **Behavior Under DB Stall** | **Unbounded Blocking** | **Clean Load Shed (HTTP 503)** | **Multi-partition Draining** | **Resilient** | **Resilient** | **Resilient** |
| **Peak Goroutines Under Overload** | **Uncapped (Piles up)** | **Strictly Bounded** | **Strictly Bounded** | **Strictly Bounded** | **Strictly Bounded** | **Strictly Bounded** |
| **Runtime Diagnostics (`pprof`)** | None | None | None | **Live Flamegraphs** | **Profile-Guided Optimization** | **Production Monitored** |
| **Sustained Load (RPS)** | Untested | Untested | Untested | Untested | Untested | *Target > 25,000 RPS* |

---

## Phase 0: Baseline Audit (Current State)

### 1. HTTP Ingestion Hot Path Micro-Benchmarks
*Environment: Windows amd64, 11th Gen Intel(R) Core(TM) i7-11800H @ 2.30GHz, 16 Threads, Go 1.25*

```text
pkg: github.com/Yakov-Gorochovsky/project/internal/handler
BenchmarkHandleIngest_Sequential-16       226,592 ops       5,320 ns/op      7,477 B/op      48 allocs/op
BenchmarkHandleIngest_Parallel-16         286,641 ops       5,294 ns/op      7,505 B/op      48 allocs/op
```

#### Key Findings:
- **High Heap Allocation Rate**: Every single incoming HTTP telemetry log allocates **48 separate heap objects** (~7.5 KB). Under a production load of 20,000 requests/sec, this produces **~150 MB/sec of heap garbage**, triggering aggressive garbage collection (GC) cycles and CPU spikes.
- **Root Cause**: `json.NewDecoder(r.Body).Decode(&payload)` into `map[string]interface{}` without buffer pooling, and struct-level reflection via validator allocating on every request.

---

### 2. Ingestion Worker Baseline
*Environment: Windows amd64, 16 Threads*

```text
pkg: github.com/Yakov-Gorochovsky/project/internal/worker
BenchmarkIngester_Enqueue-16            7,170,379 ops       164.7 ns/op          0 B/op       0 allocs/op
```

#### Key Findings:
- Under nominal conditions where the downstream buffer is freely draining, the buffered channel enqueue is fast (164.7 ns/op) with zero allocations.
- **The Backpressure Vulnerability**: In [internal/worker/ingester.go](file:///c:/Dev/Golang/project/internal/worker/ingester.go#L43), `Enqueue` is implemented as:
  ```go
  func (i *Ingester) Enqueue(log model.TelemetryLog) {
      i.logChan <- log // BLOCKING CALL!
  }
  ```
  `logChan` is bounded to `batchSize * 2` (1,000 items). If the PostgreSQL database slows down, the channel saturates. When full, **all incoming HTTP request goroutines block indefinitely** inside the handler. The server exhausts file descriptors, connections timeout, and memory climbs until potential crash.
- **Single-Consumer Bottleneck**: Only **1 consumer goroutine** runs `startConsumer()`. Database flush latency directly throttles total ingestion capacity.

---

### 3. Observability Baseline
- **`pprof`**: Not exposed. Cannot profile CPU flamegraphs, heap allocation graphs, or block contention during live execution.
- **Runtime Metrics**: No telemetry on current queue depth, dropped requests count, or batch flush duration percentiles.

---

## Planned Phases & Target Deliverables

### Phase 1: Bounded Backpressure & Load Shedding
* **Goal**: Prevent server lockup under saturation or database degradation.
* **Implementation**:
  - Non-blocking `Enqueue` with saturation contract (`select` + `default`).
  - Handler returns `HTTP 503 Service Unavailable` with `Retry-After: 1` when the queue is saturated.
  - Expose atomic queue saturation counters (`EnqueuedTotal`, `DroppedTotal`).
* **Verification**: Saturation stress test proving that HTTP goroutines remain bounded and requests fail fast (< 1ms) rather than hanging.

### Phase 2: Worker Pool Concurrency
* **Goal**: Scale batch persistence throughput across multiple concurrent workers.
* **Implementation**:
  - Configurable worker pool (N worker goroutines) draining the ingestion buffer.
  - Safe concurrent batch aggregation with coordinated channel partitioning and graceful drain on `Stop()`.
* **Verification**: Measure batch flush throughput and queue drain speed scaling across 1 vs 4 vs 8 workers.

### Phase 3: Runtime Diagnostics & `pprof`
* **Goal**: Expose live debugging and diagnostic endpoints for performance auditing.
* **Implementation**:
  - Add diagnostic server hosting `net/http/pprof` (goroutine, heap, profile, block).
  - Add Prometheus / internal metric counters for queue depth and flush latency.
* **Verification**: Capture Phase 3 CPU and Heap flamegraph artifacts.

### Phase 4: Zero-Allocation Hot Path (`sync.Pool`)
* **Goal**: Minimize heap allocations and GC pause times.
* **Implementation**:
  - Implement `sync.Pool` for byte buffers and JSON decoders.
  - Optimize payload validation hot path.
* **Verification**: Run `benchstat` against Phase 0 baseline: target $\ge 80\%$ reduction in `B/op` and reduction from 48 to $< 5$ `allocs/op`.

### Phase 5: High-Concurrency Load Test (Empirical Proof)
* **Goal**: Stress test the full end-to-end service with a reproducible load harness (`hey` or `k6`).
* **Implementation**:
  - Provide automated benchmark script simulating 50,000 requests across 100+ concurrent clients.
* **Verification**: Publish final throughput (RPS), p50, p95, p99 latency curve directly into `README.md`.

---

## Reproducing the Benchmarks

Run the handler micro-benchmarks:
```bash
go test -bench=BenchmarkHandleIngest -benchmem -run=^$ ./internal/handler/...
```

Run the worker micro-benchmarks:
```bash
go test -bench=BenchmarkIngester -benchmem -run=^$ ./internal/worker/...
```
