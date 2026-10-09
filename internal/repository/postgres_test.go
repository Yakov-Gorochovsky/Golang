package repository_test

import (
	"testing"
	"time"

	"github.com/Yakov-Gorochovsky/project/internal/repository"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPoolConfig_Bottleneck_DefaultSettingsLackWorkerAdaptability(t *testing.T) {
	dbURL := "postgres://user:password@localhost:5432/telemetry?sslmode=disable"

	// Default pgxpool configuration without hardening
	rawCfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("failed to parse test db url: %v", err)
	}

	workerCount := 16

	t.Logf("[HARDENING EVIDENCE 3 BASELINE] Default MaxConns: %d, Default MinConns: %d, Workers: %d",
		rawCfg.MaxConns, rawCfg.MinConns, workerCount)

	// In the unhardened default, MinConns is 0 (cold start connection stalls)
	// and MaxConns does not adapt to high worker count (e.g. 16 workers).
	if rawCfg.MinConns != 0 {
		t.Errorf("expected default MinConns to be 0, got %d", rawCfg.MinConns)
	}

	// Verify that our hardened NewPoolConfig dynamically scales to workers
	hardenedCfg, err := repository.NewPoolConfig(dbURL, workerCount)
	if err != nil {
		t.Fatalf("failed to build hardened pool config: %v", err)
	}

	if hardenedCfg.MaxConns < int32(workerCount*2) {
		t.Fatalf("expected MaxConns >= %d for %d workers, got %d", workerCount*2, workerCount, hardenedCfg.MaxConns)
	}
	if hardenedCfg.MinConns < int32(workerCount) {
		t.Fatalf("expected MinConns >= %d for %d workers, got %d", workerCount, workerCount, hardenedCfg.MinConns)
	}
	if hardenedCfg.MaxConnIdleTime < 1*time.Minute || hardenedCfg.MaxConnLifetime < 1*time.Minute {
		t.Fatalf("expected bounded connection lifetime and idle timeouts")
	}
	if hardenedCfg.HealthCheckPeriod == 0 {
		t.Fatalf("expected active health check period for connection eviction")
	}
}
