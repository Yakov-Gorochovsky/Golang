package server_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/Yakov-Gorochovsky/project/internal/server"
)

func TestNewServer_Bottleneck_SlowlorisProtectionEnforced(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	// Default standard library server without hardening has ReadHeaderTimeout = 0
	unhardened := &http.Server{
		Addr:    ":8080",
		Handler: dummyHandler,
	}

	t.Logf("[HARDENING EVIDENCE 4 BASELINE] Unhardened Server ReadHeaderTimeout: %v, MaxHeaderBytes: %d",
		unhardened.ReadHeaderTimeout, unhardened.MaxHeaderBytes)

	if unhardened.ReadHeaderTimeout != 0 {
		t.Fatalf("expected unhardened server to have 0 ReadHeaderTimeout")
	}

	// Verify that production server factory enforces network security timeouts
	srv := server.New(dummyHandler, "8080")

	if srv.ReadHeaderTimeout == 0 {
		t.Fatalf("VULNERABILITY DETECTED: ReadHeaderTimeout must be > 0 to prevent Slowloris attacks, got %v", srv.ReadHeaderTimeout)
	}
	if srv.ReadHeaderTimeout > 5*time.Second {
		t.Errorf("ReadHeaderTimeout should be aggressive (<= 5s), got %v", srv.ReadHeaderTimeout)
	}
	if srv.MaxHeaderBytes == 0 {
		t.Fatalf("MaxHeaderBytes must be explicitly bounded to prevent header flood attacks")
	}
	if srv.ReadTimeout == 0 || srv.WriteTimeout == 0 || srv.IdleTimeout == 0 {
		t.Fatalf("all HTTP transport timeouts must be explicitly set")
	}
}
