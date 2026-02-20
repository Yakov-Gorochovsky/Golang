package repository

import (
	"context"
	"fmt"

	"github.com/Yakov-Gorochovsky/project/internal/model" // Note: This assumes the module name is 'project'

	"github.com/jackc/pgx/v5/pgxpool"
)

// TelemetryRepository defines the interface for data access.
// Using an interface makes mocking for unit tests easy.
type TelemetryRepository interface {
	SaveLog(ctx context.Context, log model.TelemetryLog) error
	SaveLogsBatch(ctx context.Context, logs []model.TelemetryLog) error
}

// PostgresTelemetryRepo is the concrete implementation of TelemetryRepository.
type PostgresTelemetryRepo struct {
	pool *pgxpool.Pool
}

// NewPostgresTelemetryRepo creates a new Postgres backed repository.
func NewPostgresTelemetryRepo(pool *pgxpool.Pool) *PostgresTelemetryRepo {
	return &PostgresTelemetryRepo{
		pool: pool,
	}
}

// SaveLog persists a telemetry event into the database.
func (r *PostgresTelemetryRepo) SaveLog(ctx context.Context, log model.TelemetryLog) error {
	query := `
		INSERT INTO telemetry_logs (device_id, event_type, payload)
		VALUES ($1, $2, $3)
	`
	// pgx natively understands how to map map[string]interface{} to JSONB.
	// We use Exec instead of Query because we don't need to return rows.
	_, err := r.pool.Exec(ctx, query, log.DeviceID, log.EventType, log.Payload)
	if err != nil {
		return fmt.Errorf("failed to insert telemetry log: %w", err)
	}
	return nil
}

// SaveLogsBatch efficiently bulk-inserts a slice of telemetry logs.
func (r *PostgresTelemetryRepo) SaveLogsBatch(ctx context.Context, logs []model.TelemetryLog) error {
	if len(logs) == 0 {
		return nil
	}

	// This is pgx's highly optimized way of doing multi-row bulk inserts without massive round-trips
	// or building giant string queries manually.
	logsToCopy := pgxCopyFromLogs(logs)
	copyCount, copyErr := r.pool.CopyFrom(
		ctx,
		[]string{"telemetry_logs"},
		[]string{"device_id", "event_type", "payload"},
		&logsToCopy,
	)

	if copyErr != nil {
		return fmt.Errorf("failed to batch insert telemetry logs: %w", copyErr)
	}
	_ = copyCount // We don't actually need to return the count for this API
	return nil
}

// pgxCopyFromLogs satisfies the pgx.CopyFromSource interface
type pgxCopyFromLogs []model.TelemetryLog

func (p pgxCopyFromLogs) Next() bool {
	return len(p) > 0
}

func (p pgxCopyFromLogs) Values() ([]any, error) {
	if len(p) == 0 {
		return nil, nil // Should not happen if Next() returned true
	}
	row := []any{p[0].DeviceID, p[0].EventType, p[0].Payload}
	return row, nil
}

func (p *pgxCopyFromLogs) Err() error {
	*p = (*p)[1:] // Pop the first element natively
	return nil
}
