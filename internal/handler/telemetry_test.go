package handler_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Yakov-Gorochovsky/project/internal/handler"
	"github.com/Yakov-Gorochovsky/project/internal/model"
)

type MockIngester struct {
	EnqueuedLogs []model.TelemetryLog
}

func (m *MockIngester) Enqueue(log model.TelemetryLog) bool {
	m.EnqueuedLogs = append(m.EnqueuedLogs, log)
	return true
}

func TestHandleIngest(t *testing.T) {
	mockIngester := &MockIngester{}
	h := handler.NewTelemetryHandler(mockIngester)
	handlerFunc := http.HandlerFunc(h.HandleIngest)

	tests := []struct {
		name           string
		requestBody    string
		expectedStatus int
		expectInBody   string
	}{
		{
			name:           "Valid Payload",
			requestBody:    `{"device_id":"device-123","event_type":"login_success","payload":{"ip":"192.168.1.1"}}`,
			expectedStatus: http.StatusCreated,
			expectInBody:   `"status":"success"`,
		},
		{
			name:           "Invalid JSON",
			requestBody:    `{"device_id": "device-123" "missing_comma"}`,
			expectedStatus: http.StatusBadRequest,
			expectInBody:   `invalid json`,
		},
		{
			name:           "Missing Required Field",
			requestBody:    `{"device_id":"device-123","payload":{}}`,
			expectedStatus: http.StatusBadRequest,
			expectInBody:   `validation failed`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, "/telemetry", strings.NewReader(tc.requestBody))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")

			recorder := httptest.NewRecorder()
			handlerFunc.ServeHTTP(recorder, req)

			if recorder.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, recorder.Code)
			}

			if !bytes.Contains(recorder.Body.Bytes(), []byte(tc.expectInBody)) {
				t.Errorf("Expected body to contain '%s', got '%s'", tc.expectInBody, recorder.Body.String())
			}
		})
	}
}

type saturatedIngester struct{}

func (s *saturatedIngester) Enqueue(log model.TelemetryLog) bool {
	return false
}

func TestHandleIngest_BackpressureLoadShedding(t *testing.T) {
	h := handler.NewTelemetryHandler(&saturatedIngester{})
	handlerFunc := http.HandlerFunc(h.HandleIngest)

	req := httptest.NewRequest(http.MethodPost, "/telemetry", strings.NewReader(`{"device_id":"dev-1","event_type":"ping","payload":{"ok":true}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// Warm up validator reflection caches
	warmupReq := httptest.NewRequest(http.MethodPost, "/telemetry", strings.NewReader(`{"device_id":"dev-1","event_type":"ping","payload":{"ok":true}}`))
	warmupReq.Header.Set("Content-Type", "application/json")
	handlerFunc.ServeHTTP(httptest.NewRecorder(), warmupReq)

	start := time.Now()
	handlerFunc.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d on queue saturation, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	if retryAfter := rec.Header().Get("Retry-After"); retryAfter != "1" {
		t.Errorf("expected Retry-After header '1', got '%s'", retryAfter)
	}

	if status := rec.Header().Get("X-Backpressure-Status"); status != "saturated" {
		t.Errorf("expected X-Backpressure-Status header 'saturated', got '%s'", status)
	}

	if elapsed > 10*time.Millisecond {
		t.Errorf("expected fast-rejection under saturation (<10ms), took %v", elapsed)
	}
}
