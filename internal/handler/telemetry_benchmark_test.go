package handler_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Yakov-Gorochovsky/project/internal/handler"
	"github.com/Yakov-Gorochovsky/project/internal/model"
)

type atomicBenchIngester struct {
	enqueuedCount atomic.Uint64
}

func (a *atomicBenchIngester) Enqueue(log model.TelemetryLog) bool {
	a.enqueuedCount.Add(1)
	return true
}

func BenchmarkHandleIngest_Sequential(b *testing.B) {
	h := handler.NewTelemetryHandler(&atomicBenchIngester{})
	handlerFunc := http.HandlerFunc(h.HandleIngest)
	payload := []byte(`{"device_id":"sensor-bench-01","event_type":"telemetry_update","payload":{"speed":104.2,"rpm":3400,"temp":82.1}}`)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handlerFunc.ServeHTTP(rec, req)
	}
}

func BenchmarkHandleIngest_Parallel(b *testing.B) {
	h := handler.NewTelemetryHandler(&atomicBenchIngester{})
	handlerFunc := http.HandlerFunc(h.HandleIngest)
	payload := []byte(`{"device_id":"sensor-bench-01","event_type":"telemetry_update","payload":{"speed":104.2,"rpm":3400,"temp":82.1}}`)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handlerFunc.ServeHTTP(rec, req)
		}
	})
}
