package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Yakov-Gorochovsky/project/internal/model"
	"github.com/go-playground/validator/v10"
)

// maxPayloadBytes limits telemetry payload to 64KB to prevent OOM memory exhaustion.
const maxPayloadBytes = 64 << 10

// Ingester represents the sink interface required for log ingestion.
type Ingester interface {
	Enqueue(log model.TelemetryLog) bool
}

// TelemetryHandler handles telemetry ingestion HTTP requests.
type TelemetryHandler struct {
	ingester Ingester
	validate *validator.Validate
}

// NewTelemetryHandler constructs a TelemetryHandler with dependencies.
func NewTelemetryHandler(ingester Ingester) *TelemetryHandler {
	return &TelemetryHandler{
		ingester: ingester,
		validate: validator.New(),
	}
}

// HandleIngest processes incoming POST telemetry events.
func (h *TelemetryHandler) HandleIngest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Problem: Unbounded request bodies allow attackers or misconfigured agents to trigger OOM via multi-megabyte payloads.
	// Solution: Wrap r.Body in http.MaxBytesReader (64KB cap) and fast-reject oversized bodies with HTTP 413.
	r.Body = http.MaxBytesReader(w, r.Body, maxPayloadBytes)
	defer r.Body.Close()

	var payload model.TelemetryLog
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			slog.Warn("Payload exceeded maximum allowed size", "limit_bytes", maxPayloadBytes)
			h.respondError(w, http.StatusRequestEntityTooLarge, "payload too large (max 64KB)")
			return
		}
		slog.Warn("Received invalid JSON payload")
		h.respondError(w, http.StatusBadRequest, "invalid json format")
		return
	}

	if err := h.validate.Struct(payload); err != nil {
		slog.Warn("Validation failed", "error", err, "device_id", payload.DeviceID)
		h.respondError(w, http.StatusBadRequest, "validation failed: "+err.Error())
		return
	}

	if !h.ingester.Enqueue(payload) {
		slog.Warn("Ingestion queue saturated, shedding load",
			"device_id", payload.DeviceID,
			"event_type", payload.EventType)
		w.Header().Set("Retry-After", "1")
		w.Header().Set("X-Backpressure-Status", "saturated")
		h.respondError(w, http.StatusServiceUnavailable, "service unavailable: ingestion queue saturated")
		return
	}

	slog.Debug("Enqueued log event",
		"device_id", payload.DeviceID,
		"event_type", payload.EventType,
		"process_time_ms", time.Since(start).Milliseconds())

	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"status":"success"}`))
}

func (h *TelemetryHandler) respondError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
