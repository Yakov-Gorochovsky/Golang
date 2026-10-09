package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/Yakov-Gorochovsky/project/internal/model"
	"github.com/go-playground/validator/v10"
)

// Ingester represents the sink interface required for log ingestion.
type Ingester interface {
	Enqueue(log model.TelemetryLog)
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
	var payload model.TelemetryLog

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		slog.Warn("Received invalid JSON payload")
		h.respondError(w, http.StatusBadRequest, "invalid json format")
		return
	}
	defer r.Body.Close()

	if err := h.validate.Struct(payload); err != nil {
		slog.Warn("Validation failed", "error", err, "device_id", payload.DeviceID)
		h.respondError(w, http.StatusBadRequest, "validation failed: "+err.Error())
		return
	}

	h.ingester.Enqueue(payload)

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
