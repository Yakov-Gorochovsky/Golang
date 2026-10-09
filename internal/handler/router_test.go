package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Yakov-Gorochovsky/project/internal/handler"
	"github.com/Yakov-Gorochovsky/project/internal/model"
)

type mockPinger struct {
	err error
}

func (m *mockPinger) Ping(ctx context.Context) error {
	return m.err
}

type noopIngester struct{}

func (n *noopIngester) Enqueue(log model.TelemetryLog) bool { return true }

func TestRouter_Bottleneck_DatabaseReadinessSegregation(t *testing.T) {
	h := handler.NewTelemetryHandler(&noopIngester{})

	// Simulate broken database
	failingDB := &mockPinger{err: errors.New("database connection refused")}
	router := handler.NewRouterWithPinger(h, failingDB)

	// Liveness must remain 200 OK to avoid container restart loop
	reqLive := httptest.NewRequest(http.MethodGet, "/live", nil)
	recLive := httptest.NewRecorder()
	router.ServeHTTP(recLive, reqLive)

	t.Logf("[HARDENING EVIDENCE 5 BASELINE] Failing DB - /live returned HTTP %d", recLive.Code)
	if recLive.Code != http.StatusOK {
		t.Fatalf("expected /live to return 200 OK, got %d", recLive.Code)
	}

	// Readiness MUST return 503 Service Unavailable so Kubernetes drops pod from ingress
	reqReady := httptest.NewRequest(http.MethodGet, "/ready", nil)
	recReady := httptest.NewRecorder()
	router.ServeHTTP(recReady, reqReady)

	t.Logf("[HARDENING EVIDENCE 5 BASELINE] Failing DB - /ready returned HTTP %d", recReady.Code)
	if recReady.Code != http.StatusServiceUnavailable {
		t.Fatalf("VULNERABILITY DETECTED: expected /ready to return 503 when DB down, got %d", recReady.Code)
	}

	// Now simulate recovered database
	healthyDB := &mockPinger{err: nil}
	healthyRouter := handler.NewRouterWithPinger(h, healthyDB)

	recReadyHealthy := httptest.NewRecorder()
	healthyRouter.ServeHTTP(recReadyHealthy, reqReady)
	if recReadyHealthy.Code != http.StatusOK {
		t.Fatalf("expected /ready to return 200 when DB healthy, got %d", recReadyHealthy.Code)
	}
}
