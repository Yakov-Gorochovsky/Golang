package repository

import (
	"context"
	"sync"

	"github.com/Yakov-Gorochovsky/project/internal/model"
)

// MemoryTelemetryRepo provides a thread-safe in-memory repository for local development,
// offline testing, and micro-benchmarking without requiring an external PostgreSQL instance.
type MemoryTelemetryRepo struct {
	mu           sync.Mutex
	logs         []model.TelemetryLog
	flushCount   int
	simulatePing error
}

// NewMemoryTelemetryRepo constructs a new in-memory mock repository.
func NewMemoryTelemetryRepo() *MemoryTelemetryRepo {
	return &MemoryTelemetryRepo{
		logs: make([]model.TelemetryLog, 0, 1024),
	}
}

// SaveLog persists a single telemetry event to memory.
func (m *MemoryTelemetryRepo) SaveLog(ctx context.Context, log model.TelemetryLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, log)
	return nil
}

// SaveLogsBatch appends a batch of telemetry events to memory.
func (m *MemoryTelemetryRepo) SaveLogsBatch(ctx context.Context, logs []model.TelemetryLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushCount++
	m.logs = append(m.logs, logs...)
	return nil
}

// Ping implements handler.Pinger to verify health readiness.
func (m *MemoryTelemetryRepo) Ping(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.simulatePing
}

// SetPingError allows unit tests to simulate database readiness failures.
func (m *MemoryTelemetryRepo) SetPingError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.simulatePing = err
}

// Count returns the total number of preserved logs.
func (m *MemoryTelemetryRepo) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.logs)
}

// FlushCount returns the total number of batch flushes executed.
func (m *MemoryTelemetryRepo) FlushCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.flushCount
}
