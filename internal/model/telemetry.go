package model

import "time"

// TelemetryLog represents an incoming telemetry event payload.
type TelemetryLog struct {
	// DeviceID identifies the source device or sensor.
	DeviceID string `json:"device_id" validate:"required,max=255"`

	// EventType categorizes the telemetry signal (e.g., "auth_failure", "heartbeat").
	EventType string `json:"event_type" validate:"required,max=100"`

	// Payload contains arbitrary event metadata persisted as JSONB.
	Payload map[string]interface{} `json:"payload" validate:"required"`

	// CreatedAt records event persistence time.
	CreatedAt time.Time `json:"created_at,omitempty"`
}
