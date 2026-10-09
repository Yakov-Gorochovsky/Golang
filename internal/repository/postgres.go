package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Yakov-Gorochovsky/project/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TelemetryRepository defines data access methods for telemetry events.
type TelemetryRepository interface {
	SaveLog(ctx context.Context, log model.TelemetryLog) error
	SaveLogsBatch(ctx context.Context, logs []model.TelemetryLog) error
}

// PostgresTelemetryRepo persists telemetry events to PostgreSQL.
type PostgresTelemetryRepo struct {
	pool *pgxpool.Pool
}

// Problem: Default connection pool configs don't adapt to machine core counts or worker concurrency, causing starvation or connection spikes.
// Solution: Scale MaxConns to 2x worker count, pre-warm MinConns, and set aggressive keepalives to evict stale idle sockets.
func NewPoolConfig(databaseURL string, workerCount int) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database connection string: %w", err)
	}

	if workerCount < 2 {
		workerCount = 2
	}

	cfg.MaxConns = int32(workerCount * 2)
	cfg.MinConns = int32(workerCount)
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 1 * time.Minute

	return cfg, nil
}

// NewPostgresTelemetryRepo constructs a Postgres-backed telemetry repository.
func NewPostgresTelemetryRepo(pool *pgxpool.Pool) *PostgresTelemetryRepo {
	return &PostgresTelemetryRepo{
		pool: pool,
	}
}

// SaveLog persists a single telemetry event.
func (r *PostgresTelemetryRepo) SaveLog(ctx context.Context, log model.TelemetryLog) error {
	query := `
		INSERT INTO telemetry_logs (device_id, event_type, payload)
		VALUES ($1, $2, $3)
	`
	_, err := r.pool.Exec(ctx, query, log.DeviceID, log.EventType, log.Payload)
	if err != nil {
		return fmt.Errorf("failed to insert telemetry log: %w", err)
	}
	return nil
}

// SaveLogsBatch bulk-inserts logs using PostgreSQL's binary COPY protocol.
func (r *PostgresTelemetryRepo) SaveLogsBatch(ctx context.Context, logs []model.TelemetryLog) error {
	if len(logs) == 0 {
		return nil
	}

	logsToCopy := pgxCopyFromLogs(logs)
	_, err := r.pool.CopyFrom(
		ctx,
		[]string{"telemetry_logs"},
		[]string{"device_id", "event_type", "payload"},
		&logsToCopy,
	)
	if err != nil {
		return fmt.Errorf("failed to batch insert telemetry logs: %w", err)
	}
	return nil
}

type pgxCopyFromLogs []model.TelemetryLog

func (p pgxCopyFromLogs) Next() bool {
	return len(p) > 0
}

func (p pgxCopyFromLogs) Values() ([]any, error) {
	if len(p) == 0 {
		return nil, nil
	}
	row := []any{p[0].DeviceID, p[0].EventType, p[0].Payload}
	return row, nil
}

func (p *pgxCopyFromLogs) Err() error {
	*p = (*p)[1:]
	return nil
}
