package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Yakov-Gorochovsky/project/internal/middleware"
	"github.com/go-redis/redismock/v9"
)

// sentinel next handler that records whether it was called.
type nextHandler struct{ called bool }

func (n *nextHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n.called = true
	w.WriteHeader(http.StatusOK)
}

// buildRequest is a small helper to create a request with a known remote addr.
func buildRequest(remoteAddr string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	return req
}

// ─── Table-driven tests ───────────────────────────────────────────────────────

func TestRateLimit_SlidingWindow(t *testing.T) {
	const (
		testLimit  = 3
		testWindow = 60 * time.Second
		clientIP   = "192.0.2.1:54321"
	)

	cfg := middleware.RateLimitConfig{
		Limit:  testLimit,
		Window: testWindow,
	}

	tests := []struct {
		name string
		// mockSetup configures what the Redis mock expects and returns for the
		// Lua script call. The script is called with:
		//   sha    = SHA1 hex of the sliding-window Lua script (matched with .*)
		//   KEYS   = []string{rate-limit key}  (e.g. "ratelimit:192.0.2.1")
		//   ARGV[1] = current Unix-millisecond timestamp (int64)
		//   ARGV[2] = window size in milliseconds        (int64)
		//   ARGV[3] = max allowed requests               (int64)
		// Returns an integer: the current request count after adding the new request.
		mockSetup      func(mock redismock.ClientMock, key string, nowMs, windowMs, limit int64, reqID string)
		remoteAddr     string
		wantStatusCode int
		wantNextCalled bool
	}{
		{
			name: "first_request_always_allowed",
			mockSetup: func(mock redismock.ClientMock, key string, nowMs, windowMs, limit int64, reqID string) {
				// Script returns 1 → first request in the window.
				// Use Regexp() so the SHA (".*") matches whatever the implementation loads.
				mock.Regexp().ExpectEvalSha(".*", []string{key}, nowMs, windowMs, limit, reqID).
					SetVal(int64(1))
			},
			remoteAddr:     clientIP,
			wantStatusCode: http.StatusOK,
			wantNextCalled: true,
		},
		{
			name: "request_at_exact_limit_allowed",
			mockSetup: func(mock redismock.ClientMock, key string, nowMs, windowMs, limit int64, reqID string) {
				// Script returns exactly the limit – still allowed (≤ limit).
				mock.Regexp().ExpectEvalSha(".*", []string{key}, nowMs, windowMs, limit, reqID).
					SetVal(limit)
			},
			remoteAddr:     clientIP,
			wantStatusCode: http.StatusOK,
			wantNextCalled: true,
		},
		{
			name: "request_over_limit_rejected_with_429",
			mockSetup: func(mock redismock.ClientMock, key string, nowMs, windowMs, limit int64, reqID string) {
				// Script returns limit+1 → request exceeds window budget.
				mock.Regexp().ExpectEvalSha(".*", []string{key}, nowMs, windowMs, limit, reqID).
					SetVal(limit + 1)
			},
			remoteAddr:     clientIP,
			wantStatusCode: http.StatusTooManyRequests,
			wantNextCalled: false,
		},
		{
			name: "far_over_limit_still_rejected_with_429",
			mockSetup: func(mock redismock.ClientMock, key string, nowMs, windowMs, limit int64, reqID string) {
				mock.Regexp().ExpectEvalSha(".*", []string{key}, nowMs, windowMs, limit, reqID).
					SetVal(limit + 100)
			},
			remoteAddr:     clientIP,
			wantStatusCode: http.StatusTooManyRequests,
			wantNextCalled: false,
		},
		{
			name: "redis_error_fails_open_allows_request",
			mockSetup: func(mock redismock.ClientMock, key string, nowMs, windowMs, limit int64, reqID string) {
				// Simulates Redis being unreachable; middleware must fail-open.
				mock.Regexp().ExpectEvalSha(".*", []string{key}, nowMs, windowMs, limit, reqID).
					SetErr(errRedisUnavailable)
			},
			remoteAddr:     clientIP,
			wantStatusCode: http.StatusOK,
			wantNextCalled: true,
		},
		{
			name: "different_clients_use_separate_keys",
			// Each client IP gets its own counter; this case only tests the second client.
			mockSetup: func(mock redismock.ClientMock, key string, nowMs, windowMs, limit int64, reqID string) {
				mock.Regexp().ExpectEvalSha(".*", []string{key}, nowMs, windowMs, limit, reqID).
					SetVal(int64(1))
			},
			remoteAddr:     "10.0.0.2:9999", // different IP from clientIP
			wantStatusCode: http.StatusOK,
			wantNextCalled: true,
		},
		{
			name: "x_forwarded_for_overrides_remote_addr",
			mockSetup: func(mock redismock.ClientMock, _ string, nowMs, windowMs, limit int64, reqID string) {
				// The key must be derived from the X-Forwarded-For value, not RemoteAddr.
				forwardedKey := middleware.RateLimitKey("203.0.113.42")
				mock.Regexp().ExpectEvalSha(".*", []string{forwardedKey}, nowMs, windowMs, limit, reqID).
					SetVal(int64(1))
			},
			remoteAddr:     "127.0.0.1:12345", // proxy address
			wantStatusCode: http.StatusOK,
			wantNextCalled: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange ──────────────────────────────────────────────────────────
			rdb, mock := redismock.NewClientMock()
			t.Cleanup(func() {
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Errorf("unfulfilled Redis mock expectations: %v", err)
				}
			})

			// Freeze time so the test is deterministic.
			now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
			nowMs := now.UnixMilli()
			windowMs := cfg.Window.Milliseconds()
			limit := int64(cfg.Limit)

			// Build the expected Redis key from the test's remote addr.
			// For the X-Forwarded-For test the mockSetup overrides key itself.
			key := middleware.RateLimitKey(middleware.ExtractClientIP(tc.remoteAddr, ""))
			reqID := "test-req-id"
			tc.mockSetup(mock, key, nowMs, windowMs, limit, reqID)

			// Build handler chain: ratelimit middleware wrapping nextHandler.
			next := &nextHandler{}
			handler := middleware.RateLimit(rdb, cfg, middleware.WithNowFunc(func() time.Time { return now }), middleware.WithReqIDFunc(func() string { return reqID }))(next)

			// Build request.
			req := buildRequest(tc.remoteAddr)
			if tc.name == "x_forwarded_for_overrides_remote_addr" {
				req.Header.Set("X-Forwarded-For", "203.0.113.42")
			}
			rec := httptest.NewRecorder()

			// Act ──────────────────────────────────────────────────────────────
			handler.ServeHTTP(rec, req)

			// Assert ───────────────────────────────────────────────────────────
			if rec.Code != tc.wantStatusCode {
				t.Errorf("status: got %d, want %d", rec.Code, tc.wantStatusCode)
			}
			if next.called != tc.wantNextCalled {
				t.Errorf("next handler called: got %v, want %v", next.called, tc.wantNextCalled)
			}
		})
	}
}

// TestRateLimit_ResponseHeaders verifies that the middleware sets the standard
// rate-limit headers so clients know their quota state.
func TestRateLimit_ResponseHeaders(t *testing.T) {
	cfg := middleware.RateLimitConfig{Limit: 5, Window: 60 * time.Second}
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	nowMs := now.UnixMilli()
	windowMs := cfg.Window.Milliseconds()

	tests := []struct {
		name              string
		scriptReturnCount int64
		wantRemaining     string // X-RateLimit-Remaining value
	}{
		{
			name:              "first_request_remaining_is_limit_minus_one",
			scriptReturnCount: 1,
			wantRemaining:     "4", // limit(5) - count(1) = 4
		},
		{
			name:              "at_limit_remaining_is_zero",
			scriptReturnCount: 5,
			wantRemaining:     "0",
		},
		{
			name:              "over_limit_remaining_stays_zero_not_negative",
			scriptReturnCount: 6,
			wantRemaining:     "0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rdb, mock := redismock.NewClientMock()
			t.Cleanup(func() {
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Errorf("unfulfilled Redis expectations: %v", err)
				}
			})

			// Match any SHA; supply exact args so the mock can verify them.
			reqID := "test-req-id"
			key := middleware.RateLimitKey(middleware.ExtractClientIP("192.0.2.99:1234", ""))
			mock.Regexp().ExpectEvalSha(".*", []string{key}, nowMs, windowMs, int64(cfg.Limit), reqID).
				SetVal(tc.scriptReturnCount)

			next := &nextHandler{}
			handler := middleware.RateLimit(rdb, cfg, middleware.WithNowFunc(func() time.Time { return now }), middleware.WithReqIDFunc(func() string { return reqID }))(next)

			req := buildRequest("192.0.2.99:1234")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			// Mandatory headers
			if got := rec.Header().Get("X-RateLimit-Limit"); got != "5" {
				t.Errorf("X-RateLimit-Limit: got %q, want %q", got, "5")
			}
			if got := rec.Header().Get("X-RateLimit-Remaining"); got != tc.wantRemaining {
				t.Errorf("X-RateLimit-Remaining: got %q, want %q", got, tc.wantRemaining)
			}
			if got := rec.Header().Get("X-RateLimit-Reset"); got == "" {
				t.Error("X-RateLimit-Reset header must be present")
			}
		})
	}
}

// TestRateLimit_429Body verifies the body returned when the rate limit is hit.
func TestRateLimit_429Body(t *testing.T) {
	cfg := middleware.RateLimitConfig{Limit: 1, Window: 60 * time.Second}
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	nowMs := now.UnixMilli()
	windowMs := cfg.Window.Milliseconds()

	rdb, mock := redismock.NewClientMock()
	defer func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled Redis expectations: %v", err)
		}
	}()

	reqID := "test-req-id"
	key := middleware.RateLimitKey(middleware.ExtractClientIP("192.0.2.1:1111", ""))
	// Script says limit exceeded (count=2 > limit=1).
	mock.Regexp().ExpectEvalSha(".*", []string{key}, nowMs, windowMs, int64(cfg.Limit), reqID).
		SetVal(int64(2))

	next := &nextHandler{}
	handler := middleware.RateLimit(rdb, cfg, middleware.WithNowFunc(func() time.Time { return now }), middleware.WithReqIDFunc(func() string { return reqID }))(next)

	req := buildRequest("192.0.2.1:1111")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", ct, "application/json")
	}
	body := rec.Body.String()
	if body == "" {
		t.Error("response body must not be empty for 429")
	}
	if next.called {
		t.Error("next handler must NOT be called when rate-limited")
	}
}

// ─── Exported helpers referenced by tests ────────────────────────────────────
// These compile-time assertions document the public API the implementation must
// expose. The compiler will fail until the implementation provides them.

var _ = middleware.RateLimitKey    // func(ip string) string
var _ = middleware.ExtractClientIP // func(remoteAddr, xForwardedFor string) string

// errRedisUnavailable is a local sentinel error used to simulate Redis failure.
var errRedisUnavailable = &redisError{"connection refused"}

type redisError struct{ msg string }

func (e *redisError) Error() string { return e.msg }
