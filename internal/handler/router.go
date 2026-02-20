package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter sets up the chi router and wires up the endpoints.
func NewRouter(telemetryHandler *TelemetryHandler) *chi.Mux {
	r := chi.NewRouter()

	// Standard middleware stack
	r.Use(middleware.RequestID) // Inject a unique request ID into context
	r.Use(middleware.RealIP)    // Ensure the client IP is accurate behind proxies
	r.Use(middleware.Logger)    // Log all HTTP requests
	r.Use(middleware.Recoverer) // Automatically recover from panics so the server doesn't crash

	// Health check endpoint (very common in cloud/kubernetes deployments)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// API Version 1 Routes
	r.Route("/api/v1", func(r chi.Router) {
		// Bind the POST ingestion route to the HandleIngest method
		r.Post("/telemetry", telemetryHandler.HandleIngest)
	})

	return r
}
