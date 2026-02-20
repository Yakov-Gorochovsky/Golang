package handler_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Yakov-Gorochovsky/project/internal/handler"
	"github.com/Yakov-Gorochovsky/project/internal/model"
)

// MockIngester implements the handler.Ingester interface for testing
type MockIngester struct {
	EnqueuedLogs []model.TelemetryLog
}

func (m *MockIngester) Enqueue(log model.TelemetryLog) {
	m.EnqueuedLogs = append(m.EnqueuedLogs, log)
}

func TestHandleIngest(t *testing.T) {
	// 1. Setup minimal dependencies
	mockIngester := &MockIngester{}
	h := handler.NewTelemetryHandler(mockIngester)

	// Create an HTTP test handler based on the chi router logic in router.go
	// (or just use the handler func directly)
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
			requestBody:    `{"device_id": "device-123" "missing_comma"}`, // Broken JSON
			expectedStatus: http.StatusBadRequest,
			expectInBody:   `invalid json`,
		},
		{
			name:           "Missing Required Field (Validation Error)",
			requestBody:    `{"device_id":"device-123","payload":{}}`, // Missing 'event_type'
			expectedStatus: http.StatusBadRequest,
			expectInBody:   `validation failed`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange: Create HTTP recording tools
			req, err := http.NewRequest(http.MethodPost, "/telemetry", strings.NewReader(tc.requestBody))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")

			// We use ResponseRecorder to capture the HTTP response seamlessly
			recorder := httptest.NewRecorder()

			// Act: Serve the request
			handlerFunc.ServeHTTP(recorder, req)

			// Assert: Check results
			if recorder.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, recorder.Code)
			}

			if !bytes.Contains(recorder.Body.Bytes(), []byte(tc.expectInBody)) {
				t.Errorf("Expected body to contain '%s', got '%s'", tc.expectInBody, recorder.Body.String())
			}
		})
	}
}
