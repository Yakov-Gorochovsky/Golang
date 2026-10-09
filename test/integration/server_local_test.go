package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Yakov-Gorochovsky/project/internal/handler"
	"github.com/Yakov-Gorochovsky/project/internal/repository"
	"github.com/Yakov-Gorochovsky/project/internal/worker"
)

// TestLocalStandaloneStack verifies that the application can be instantiated,
// run locally, and tested end-to-end with zero external database dependencies.
func TestLocalStandaloneStack(t *testing.T) {
	// 1. Mock / In-Memory Backing Store
	memRepo := repository.NewMemoryTelemetryRepo()

	// 2. Worker Pool with 2 workers
	ingester := worker.NewIngester(memRepo, 10, 50*time.Millisecond, 2)
	defer ingester.Stop()

	// 3. Telemetry Handler & Router with Pinger
	h := handler.NewTelemetryHandler(ingester)
	router := handler.NewRouterWithPinger(h, memRepo)

	// 4. Test Server
	ts := httptest.NewServer(router)
	defer ts.Close()

	client := ts.Client()

	// Test A: Liveness Probe
	respLive, err := client.Get(ts.URL + "/live")
	if err != nil {
		t.Fatalf("failed GET /live: %v", err)
	}
	defer respLive.Body.Close()
	if respLive.StatusCode != http.StatusOK {
		t.Errorf("expected /live to return 200, got %d", respLive.StatusCode)
	}

	// Test B: Readiness Probe
	respReady, err := client.Get(ts.URL + "/ready")
	if err != nil {
		t.Fatalf("failed GET /ready: %v", err)
	}
	defer respReady.Body.Close()
	if respReady.StatusCode != http.StatusOK {
		t.Errorf("expected /ready to return 200, got %d", respReady.StatusCode)
	}

	// Test C: Valid Ingestion POST
	payload := `{"device_id":"local-dev-01","event_type":"engine_temp","payload":{"temp_c":88.5}}`
	respPost, err := client.Post(ts.URL+"/api/v1/telemetry", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("failed POST /api/v1/telemetry: %v", err)
	}
	defer respPost.Body.Close()
	if respPost.StatusCode != http.StatusCreated {
		t.Errorf("expected POST to return 201 Created, got %d", respPost.StatusCode)
	}

	var resBody map[string]interface{}
	if err := json.NewDecoder(respPost.Body).Decode(&resBody); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}
	if resBody["status"] != "success" {
		t.Errorf("expected status 'success', got %v", resBody["status"])
	}

	// Test D: Oversized Request Guardrail (HTTP 413)
	hugeString := strings.Repeat("MOCK", 25000) // ~100KB > 64KB limit
	hugePayload := `{"device_id":"huge-dev","event_type":"overflow","payload":{"data":"` + hugeString + `"}}`
	respHuge, err := client.Post(ts.URL+"/api/v1/telemetry", "application/json", strings.NewReader(hugePayload))
	if err != nil {
		t.Fatalf("failed POST oversized payload: %v", err)
	}
	defer respHuge.Body.Close()
	if respHuge.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("expected oversized body to return 413, got %d", respHuge.StatusCode)
	}

	// Test E: Wait for Ingester Flush & Verify In-Memory Persistence
	time.Sleep(100 * time.Millisecond)
	if memRepo.Count() != 1 {
		t.Errorf("expected 1 log persisted in memory repo, got %d", memRepo.Count())
	}
}
