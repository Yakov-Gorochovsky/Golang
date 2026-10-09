package integration_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Yakov-Gorochovsky/project/internal/handler"
	"github.com/Yakov-Gorochovsky/project/internal/model"
)

type stubIngester struct{}

func (s *stubIngester) Enqueue(log model.TelemetryLog) bool {
	return true
}

func TestHealthEndpoint(t *testing.T) {
	h := handler.NewTelemetryHandler(&stubIngester{})
	router := handler.NewRouter(h)

	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("failed to query health endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
}
