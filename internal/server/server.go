package server

import (
	"net/http"
	"time"
)

// Problem: Missing ReadHeaderTimeout leaves servers vulnerable to Slowloris attacks where slow header drips hold TCP sockets open indefinitely.
// Solution: Construct http.Server with enforced ReadHeaderTimeout (3s), bounded MaxHeaderBytes (1MB), and deterministic transport timeouts.
func New(handler http.Handler, port string) *http.Server {
	return &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB header limit
	}
}
