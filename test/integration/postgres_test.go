package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/Yakov-Gorochovsky/project/internal/model"
	"github.com/Yakov-Gorochovsky/project/internal/repository"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresRepository ensures that our repository layer interacts with the Real Database correctly.
// This requires the docker-compose postgres container to be running!
func TestPostgresRepository(t *testing.T) {
	// Setup Database Connection Pool
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use our development db setup
	dbURL := "postgres://user:password@localhost:5432/telemetry"
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("Unable to connect to database. Did you run 'docker-compose up -d'? %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("Cannot ping database. %v", err)
	}

	// Given: A Postgres Repo
	repo := repository.NewPostgresTelemetryRepo(pool)

	// And: A valid telemetry log model
	logEvent := model.TelemetryLog{
		DeviceID:  "int-test-device-01",
		EventType: "integration_test_event",
		Payload: map[string]interface{}{
			"test_mode": true,
			"severity":  "low",
		},
	}

	// Act: Save it to the database
	err = repo.SaveLog(context.Background(), logEvent)

	// Assert: Check that it succeeded
	if err != nil {
		t.Errorf("Failed to save log to postgres: %v", err)
	}

	// Act & Assert (Verification): Query it back
	var count int
	err = pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM telemetry_logs WHERE device_id = $1", logEvent.DeviceID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query inserted rows: %v", err)
	}

	if count == 0 {
		t.Errorf("Expected row to be inserted, but count was 0")
	}
}
