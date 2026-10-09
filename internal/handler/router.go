package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Pinger defines an interface for health check ping operations.
type Pinger interface {
	Ping(ctx context.Context) error
}

// NewRouter initializes the HTTP router with base middleware and routes.
func NewRouter(telemetryHandler *TelemetryHandler) *chi.Mux {
	return NewRouterWithPinger(telemetryHandler, nil)
}

// NewRouterWithPinger initializes the HTTP router with segregated liveness and readiness probes.
func NewRouterWithPinger(telemetryHandler *TelemetryHandler, pinger Pinger) *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Problem: Single monolithic /health check causes crashloop restarts during DB hiccups or routes traffic to dead pods.
	// Solution: Segregate /live (process vitality) from /ready (database connectivity probe).

	// Liveness probe: checks if process is alive and responsive without external dependencies.
	r.Get("/live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"alive"}`))
	})

	// Readiness probe: verifies backing database connectivity before ingress routes user traffic.
	r.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
		if pinger != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := pinger.Ping(ctx); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"status":"not_ready","error":"database unreachable"}`))
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})

	// Backward-compatible alias
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/telemetry", telemetryHandler.HandleIngest)
	})

	return r
}
