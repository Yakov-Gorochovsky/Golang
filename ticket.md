# GM-2 — Add a log the program has started

## Jira Metadata
| Field       | Value                        |
|-------------|------------------------------|
| Ticket ID   | GM-2                         |
| Project     | Golang and MCP (GM)          |
| Type        | Task                         |
| Priority    | Medium                       |
| Status      | To Do                        |
| Reporter    | Yakov Gorochovsky            |

## Business Description
> "We need to add a log telling us the programs started running."

Emit a structured startup log entry at program launch so that operations teams can confirm the process has initialised successfully and identify its version/configuration at a glance.

## Technical Constraints (from project rules)

### Stack
- **Language:** Go 1.21+
- **Logger:** `log/slog` (structured JSON, already wired in `cmd/server/main.go`)
- **Entry point:** `cmd/server/main.go` — `func main()`

### Coding Style
- `gofmt` / `goimports` mandatory
- Accept interfaces, return structs
- Errors wrapped with `fmt.Errorf("...: %w", err)`

### Testing
- Table-driven tests with the standard `go test` toolchain
- Always run with `-race` flag: `go test -race ./...`
- Coverage gate: `go test -cover ./...`

## Scope

### What to implement
A **single `slog.Info` call** immediately after the logger is set as the default, before any I/O or network operations:

```go
slog.Info("Server started",
    "app",     "telemetry-api",
    "version", cfg.Version,   // add Version field to Config if not present
    "port",    cfg.Port,
)
```

The log must appear **before** the DB connection attempt so the application proves it reached `main()` even if the database is unreachable.

### Current state
`main.go` line 25 already contains:
```go
slog.Info("Starting Telemetry API...", "port", cfg.Port)
```
This satisfies the ticket intent. The task is to **verify** this log is adequate, **optionally enrich** it with structured fields (`app`, `version`), and confirm it is covered by smoke/integration tests if any exist.

### Acceptance Criteria
- [x] A structured log line is emitted at startup at level `INFO`.
- [ ] Log includes: `app`, `port`, at minimum.
- [ ] `version` field sourced from config or a build-time constant.
- [ ] Unit/integration test verifies the log is produced (optional but preferred).
- [ ] `go vet ./...` passes with zero warnings.
- [ ] `go test -race ./...` passes.
