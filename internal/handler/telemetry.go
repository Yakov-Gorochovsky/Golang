package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/Yakov-Gorochovsky/project/internal/model"
	// We declare a minimal interface for what the handler requires from the worker
	// This makes testing even easier!
	"github.com/go-playground/validator/v10"
)

// Ingester declares the dependency the Handler needs.
type Ingester interface {
	Enqueue(log model.TelemetryLog)
}

// TelemetryHandler depends on an abstract worker (Ingester) to handle the logs async.
type TelemetryHandler struct {
	ingester Ingester
	validate *validator.Validate
}

// NewTelemetryHandler creates a new handler with its dependencies.
func NewTelemetryHandler(ingester Ingester) *TelemetryHandler {
	return &TelemetryHandler{
		ingester: ingester,
		validate: validator.New(),
	}
}

// HandleIngest is the HTTP handler function for incoming POST requests.
func (h *TelemetryHandler) HandleIngest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var payload model.TelemetryLog

	// 1. Decode JSON payload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		slog.Warn("Received invalid JSON payload")
		h.respondError(w, http.StatusBadRequest, "invalid json format")
		return
	}
	defer r.Body.Close()

	// 2. Validate the struct
	if err := h.validate.Struct(payload); err != nil {
		slog.Warn("Validation failed", "error", err, "device_id", payload.DeviceID)
		h.respondError(w, http.StatusBadRequest, "validation failed: "+err.Error())
		return
	}

	// 3. Enqueue to background worker instantly! (Non-Blocking)
	h.ingester.Enqueue(payload)

	// Contextual structured logging
	slog.Debug("Enqueued log event",
		"device_id", payload.DeviceID,
		"event_type", payload.EventType,
		"process_time_ms", time.Since(start).Milliseconds())

	// 4. Respond with success immediately
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(`{"status":"success"}`))
}

// respondError is a small helper function to format JSON error responses.
func (h *TelemetryHandler) respondError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
