// Package middleware provides HTTP middleware for the telemetry API.
package middleware

import (
	"context"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // SHA1 is required by the Redis EVALSHA protocol
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// ─── Sliding-window Lua script ────────────────────────────────────────────────
//
// The script implements a sliding-window counter using a Redis sorted-set.
// Each request is recorded as a scored member (score = arrival time in Unix
// milliseconds). Old entries outside the window are pruned atomically.
//
// KEYS[1]  – the per-client rate-limit key  (e.g. "ratelimit:192.0.2.1")
// ARGV[1]  – now in Unix milliseconds        (int64)
// ARGV[2]  – window duration in milliseconds (int64)
// ARGV[3]  – maximum allowed requests        (int64)
//
// Returns: current count of requests in the window AFTER adding this one.
//
// Atomicity guarantee: the entire sequence is a single Lua transaction on one
// Redis node — no race conditions are possible.
const slidingWindowScript = `local key        = KEYS[1]
local now        = tonumber(ARGV[1])
local window     = tonumber(ARGV[2])
local limit      = tonumber(ARGV[3])
local reqID      = ARGV[4]
local clearBefore = now - window

redis.call("ZREMRANGEBYSCORE", key, "-inf", clearBefore)

local count = redis.call("ZCARD", key)

if count < limit then
	redis.call("ZADD", key, now, now .. "-" .. reqID)
end
redis.call("PEXPIRE", key, window)

return count + 1
`

// slidingWindowSHA is the Redis EVALSHA SHA1 of slidingWindowScript, computed
// at init time so we never need a network round-trip to ScriptLoad.
// Redis computes SHA1 over the exact script bytes before storing it.
var slidingWindowSHA = func() string {
	//nolint:gosec
	h := sha1.New()
	h.Write([]byte(slidingWindowScript))
	return hex.EncodeToString(h.Sum(nil))
}()

// ─── Public types ─────────────────────────────────────────────────────────────

// RateLimitConfig controls the sliding-window parameters.
type RateLimitConfig struct {
	// Limit is the maximum number of requests allowed per Window.
	Limit int
	// Window is the duration of the sliding time window.
	Window time.Duration
}

// Option is a functional option for RateLimit.
type Option func(*rateLimiter)

// WithNowFunc injects a custom clock into the middleware, enabling deterministic
// unit tests without real-time dependencies.
func WithNowFunc(fn func() time.Time) Option {
	return func(rl *rateLimiter) {
		rl.now = fn
	}
}

// WithReqIDFunc injects a custom request ID generator for deterministic tests.
func WithReqIDFunc(fn func() string) Option {
	return func(rl *rateLimiter) {
		rl.reqIDFn = fn
	}
}

// ─── Internal limiter state ───────────────────────────────────────────────────

type rateLimiter struct {
	rdb     *redis.Client
	cfg     RateLimitConfig
	now     func() time.Time
	reqIDFn func() string
}

// ─── Constructor / middleware factory ─────────────────────────────────────────

// RateLimit returns a chi-compatible HTTP middleware that enforces a sliding-
// window rate limit using Redis as the distributed counter.
//
// Fail-open policy: if Redis is unreachable the request is forwarded so that a
// Redis outage does not take down the API (see rate_limiter_plan.md § Risks).
func RateLimit(rdb *redis.Client, cfg RateLimitConfig, opts ...Option) func(http.Handler) http.Handler {
	rl := &rateLimiter{
		rdb:     rdb,
		cfg:     cfg,
		now:     time.Now,
		reqIDFn: generateRequestID,
	}
	for _, o := range opts {
		o(rl)
	}
	return rl.middleware
}

// middleware is the actual http.Handler wrapper.
func (rl *rateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := rl.now()
		limit := int64(rl.cfg.Limit)
		windowMs := rl.cfg.Window.Milliseconds()
		nowMs := now.UnixMilli()

		// Derive the rate-limit key from the most accurate client identifier.
		ip := ExtractClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
		key := RateLimitKey(ip)

		// Reset time = Unix timestamp of when the current window expires.
		resetAt := now.Add(rl.cfg.Window).Unix()

		// Generate a secure payload ID to avoid ZSET member collisions
		reqID := rl.reqIDFn()

		// Write informational headers before we know the outcome — even 429
		// responses carry them so clients can plan their retry strategy.
		w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(limit, 10))
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))

		count, err := rl.runScript(r.Context(), key, nowMs, windowMs, limit, reqID)
		if err != nil {
			// Fail-open: log and let the request through.
			slog.Warn("ratelimit: Redis error, failing open", "key", key, "err", err)
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(limit, 10))
			next.ServeHTTP(w, r)
			return
		}

		remaining := limit - count
		if remaining < 0 {
			remaining = 0
		}
		w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))

		if count > limit {
			writeTooManyRequests(w)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// runScript executes the sliding-window Lua script via EVALSHA, falling back
// to EVAL if Redis reports NOSCRIPT (script cache was flushed).
func (rl *rateLimiter) runScript(ctx context.Context, key string, nowMs, windowMs, limit int64, reqID string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	// Fast path: use the pre-computed SHA (no ScriptLoad round-trip needed).
	count, err := rl.evalSHA(ctx, slidingWindowSHA, key, nowMs, windowMs, limit, reqID)
	if err == nil {
		return count, nil
	}

	// Slow path: SHA not in Redis cache — fall back to EVAL to load it implicitly.
	if isNoScriptError(err) {
		return rl.eval(ctx, key, nowMs, windowMs, limit, reqID)
	}

	return 0, err
}

func (rl *rateLimiter) evalSHA(ctx context.Context, sha, key string, nowMs, windowMs, limit int64, reqID string) (int64, error) {
	res, err := rl.rdb.EvalSha(ctx, sha, []string{key}, nowMs, windowMs, limit, reqID).Result()
	if err != nil {
		return 0, err
	}
	return toInt64(res)
}

func (rl *rateLimiter) eval(ctx context.Context, key string, nowMs, windowMs, limit int64, reqID string) (int64, error) {
	res, err := rl.rdb.Eval(ctx, slidingWindowScript, []string{key}, nowMs, windowMs, limit, reqID).Result()
	if err != nil {
		return 0, err
	}
	return toInt64(res)
}

// ─── Exported helper functions ────────────────────────────────────────────────

// RateLimitKey constructs the Redis key for a given client IP address.
// The prefix isolates rate-limit keys from other data in the same Redis instance.
func RateLimitKey(ip string) string {
	return "ratelimit:" + ip
}

// ExtractClientIP returns the most accurate client IP available.
//
// Priority:
//  1. First entry in X-Forwarded-For (set by a trusted reverse proxy)
//  2. RemoteAddr (host portion only, port stripped)
//
// The xForwardedFor argument should be r.Header.Get("X-Forwarded-For").
func ExtractClientIP(remoteAddr, xForwardedFor string) string {
	if xForwardedFor != "" {
		// X-Forwarded-For may be a comma-separated list: "client, proxy1, proxy2"
		parts := strings.SplitN(xForwardedFor, ",", 2)
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// remoteAddr may already be a bare IP with no port.
		return remoteAddr
	}
	return host
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// tooManyRequestsBody is the JSON payload returned on HTTP 429 responses.
type tooManyRequestsBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeTooManyRequests(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	body := tooManyRequestsBody{
		Error:   "too_many_requests",
		Message: "Rate limit exceeded. Please slow down and try again later.",
	}
	_ = json.NewEncoder(w).Encode(body)
}

// isNoScriptError reports whether a Redis error is a NOSCRIPT response,
// meaning the script SHA is no longer cached (e.g. after a Redis restart).
func isNoScriptError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "NOSCRIPT")
}

// toInt64 converts a Redis script return value to int64.
func toInt64(v interface{}) (int64, error) {
	n, ok := v.(int64)
	if !ok {
		return 0, fmt.Errorf("ratelimit: expected int64 from Lua script, got %T (%v)", v, v)
	}
	return n, nil
}

// generateRequestID returns a random hex string for collision prevention.
func generateRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
