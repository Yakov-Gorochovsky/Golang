package model

import "time"

// TelemetryLog represents the expected incoming JSON payload for our ingestion API.
// We use validation tags (binding rules) to ensure requests are well-formed.
type TelemetryLog struct {
	// DeviceID identifies the source. The validator ensures it's required and <= 255 chars
	DeviceID string `json:"device_id" validate:"required,max=255"`

	// EventType categorizes the log (e.g., "auth_failure", "network_scan")
	EventType string `json:"event_type" validate:"required,max=100"`

	// Payload is arbitrary JSON data associated with the event.
	// Since we don't know the structure, we map it to an empty interface map,
	// which PostgreSQL's pgx library will naturally serialize into JSONB.
	Payload map[string]interface{} `json:"payload" validate:"required"`

	// CreatedAt is set by the database usually, but we include it in the model
	// so we can scan it out if we read records later.
	CreatedAt time.Time `json:"created_at,omitempty"`
}
