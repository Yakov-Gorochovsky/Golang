package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Yakov-Gorochovsky/project/internal/model"
	"github.com/Yakov-Gorochovsky/project/internal/repository"
)

func TestMemoryTelemetryRepo_Operations(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryTelemetryRepo()

	// Initial count
	if repo.Count() != 0 {
		t.Fatalf("expected initial count 0, got %d", repo.Count())
	}

	// Single log insert
	err := repo.SaveLog(ctx, model.TelemetryLog{DeviceID: "dev-1", EventType: "boot"})
	if err != nil {
		t.Fatalf("expected nil error on SaveLog, got: %v", err)
	}
	if repo.Count() != 1 {
		t.Fatalf("expected count 1, got %d", repo.Count())
	}

	// Batch insert
	batch := []model.TelemetryLog{
		{DeviceID: "dev-2", EventType: "metric"},
		{DeviceID: "dev-3", EventType: "metric"},
	}
	err = repo.SaveLogsBatch(ctx, batch)
	if err != nil {
		t.Fatalf("expected nil error on SaveLogsBatch, got: %v", err)
	}
	if repo.Count() != 3 {
		t.Fatalf("expected count 3, got %d", repo.Count())
	}
	if repo.FlushCount() != 1 {
		t.Fatalf("expected flush count 1, got %d", repo.FlushCount())
	}

	// Ping success
	if err := repo.Ping(ctx); err != nil {
		t.Fatalf("expected nil ping error, got: %v", err)
	}

	// Ping failure simulation
	simulatedErr := errors.New("simulated network loss")
	repo.SetPingError(simulatedErr)
	if err := repo.Ping(ctx); !errors.Is(err, simulatedErr) {
		t.Fatalf("expected simulated error on ping, got: %v", err)
	}
}

func TestMemoryTelemetryRepo_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryTelemetryRepo()

	var wg sync.WaitGroup
	numGoroutines := 10
	logsPerRoutine := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			for j := 0; j < logsPerRoutine; j++ {
				_ = repo.SaveLog(ctx, model.TelemetryLog{DeviceID: "concurrent-dev", EventType: "ping"})
			}
		}(i)
	}

	wg.Wait()

	expectedTotal := numGoroutines * logsPerRoutine
	if repo.Count() != expectedTotal {
		t.Fatalf("expected %d logs after concurrent writes, got %d", expectedTotal, repo.Count())
	}
}
