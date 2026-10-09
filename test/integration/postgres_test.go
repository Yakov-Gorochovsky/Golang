//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/Yakov-Gorochovsky/project/internal/model"
	"github.com/Yakov-Gorochovsky/project/internal/repository"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresRepository(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dbURL := "postgres://user:password@localhost:5432/telemetry"
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("Unable to connect to database at %s, skipping: %v", dbURL, err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("PostgreSQL ping failed at %s, skipping integration test: %v", dbURL, err)
	}

	repo := repository.NewPostgresTelemetryRepo(pool)

	logEvent := model.TelemetryLog{
		DeviceID:  "int-test-device-01",
		EventType: "integration_test_event",
		Payload: map[string]interface{}{
			"test_mode": true,
			"severity":  "low",
		},
	}

	if err := repo.SaveLog(context.Background(), logEvent); err != nil {
		t.Fatalf("failed to insert telemetry log: %v", err)
	}

	var count int
	err = pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM telemetry_logs WHERE device_id = $1", logEvent.DeviceID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query inserted row: %v", err)
	}

	if count == 0 {
		t.Fatalf("expected at least 1 inserted row, got %d", count)
	}
}
