# GM-2 — TDD Implementation Roadmap

## Goal
Emit a structured startup log as the first observable side-effect of `main()` in `cmd/server/main.go`, confirming the service has initialised.

---

## Phase 0 — Baseline Audit (No Code Change)

**Objective:** Confirm what already exists and decide minimal delta needed.

1. Run `go vet ./...` — must be zero warnings.
2. Run `go test -race ./...` — all tests must pass.
3. Inspect `cmd/server/main.go` line 25:
   ```go
   slog.Info("Starting Telemetry API...", "port", cfg.Port)
   ```
   Verdict: already satisfies the ticket's core requirement. Enhancement is needed for `app` and `version` fields.

---

## Phase 1 — Write the Test First (TDD)

**File:** `cmd/server/main_test.go` *(new)*

Write a table-driven integration-style test that:
- Captures `os.Stdout` via a pipe or `bytes.Buffer` fed to `slog.NewJSONHandler`.
- Calls the startup log helper (extracted in Phase 2) with a known config.
- Asserts the JSON output contains:
  - `"level":"INFO"`
  - `"msg":"Server started"`
  - `"app":"telemetry-api"`
  - `"port":<expected_port>`

```go
func TestStartupLog(t *testing.T) {
    cases := []struct {
        name string
        port string
    }{
        {"default port", "8080"},
        {"custom port",  "9090"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            var buf bytes.Buffer
            logger := slog.New(slog.NewJSONHandler(&buf, nil))
            logStartup(logger, tc.port) // func under test
            got := buf.String()
            if !strings.Contains(got, `"msg":"Server started"`) {
                t.Errorf("missing startup msg; got: %s", got)
            }
            if !strings.Contains(got, tc.port) {
                t.Errorf("missing port %s; got: %s", tc.port, got)
            }
        })
    }
}
```

**Expected result:** Fails to compile (function doesn't exist yet). ✅ RED.

---

## Phase 2 — Implement the Minimal Change

**File:** `cmd/server/main.go`

1. Extract a helper to make it testable without running the full server:
   ```go
   // logStartup emits the initial "Server started" structured log entry.
   // Signature:  logStartup(logger *slog.Logger, port string)
   func logStartup(logger *slog.Logger, port string) {
       logger.Info("Server started",
           "app",  "telemetry-api",
           "port", port,
       )
   }
   ```
2. Replace the ad-hoc `slog.Info("Starting Telemetry API...", ...)` call on line 25 with:
   ```go
   logStartup(logger, cfg.Port)
   ```

**Expected result:** Test compiles and passes. ✅ GREEN.

---

## Phase 3 — Optional: Add `version` Field

**File:** `internal/config/config.go`

1. Add `Version string` field to the `Config` struct.
2. Populate from env var `APP_VERSION`, defaulting to `"dev"`.
3. Update `logStartup` signature:
   ```go
   func logStartup(logger *slog.Logger, port, version string) {
       logger.Info("Server started",
           "app",     "telemetry-api",
           "version", version,
           "port",    port,
       )
   }
   ```
4. Update test cases to supply version.

---

## Phase 4 — Refactor & Cleanup

- Run `goimports ./...` — fix any import ordering.
- Run `gofmt -w ./...` — ensure zero diffs.
- Run `go vet ./...` — zero warnings.
- Run `go test -race -cover ./...` — all tests pass; report coverage delta.

---

## Phase 5 — Done Definition

| Check                               | Tools                         |
|-------------------------------------|-------------------------------|
| `go vet` clean                      | `go vet ./...`                |
| All tests pass with race detector   | `go test -race ./...`         |
| Startup log contains required fields| Table-driven unit test        |
| `gofmt` / `goimports` clean         | `gofmt -l .` returns nothing  |
| PR ready for review                 | `git push origin feature/GM-2-add-startup-log` |
