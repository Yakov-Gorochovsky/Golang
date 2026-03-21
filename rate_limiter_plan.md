# Implementation Plan: Scalable Rate-Limiting Middleware

## Requirements Restatement
- Design a scalable rate-limiting middleware for the high-throughput Go REST API.
- Support horizontal scaling utilizing a distributed data store (Redis).
- Integrate seamlessly with the existing `go-chi/chi/v5` router.
- Allow configuration of rate limits via environment variables or configuration files.
- Ensure proper HTTP 429 Too Many Requests responses.

## Implementation Phases

### Phase 1: Infrastructure & Dependencies
- Add a `redis` service to the `docker-compose.yml` file for a distributed state store.
- Add `github.com/redis/go-redis/v9` as a project dependency using `go get`.

### Phase 2: Configuration 
- Modify `internal/config/config.go`
- Add Redis connection parameters (Host, Port, Password).
- Add rate limiter configuration (Limit, Window) to specify allowed requests per time window (e.g., 100 requests per minute).

### Phase 3: Core Rate Limiter Logic
- Create `internal/middleware/ratelimit.go`
- Implement a sliding window or fixed window counter algorithm using Redis Lua scripts for atomic operations to prevent race conditions.
- Create the HTTP middleware function: `func RateLimit(rdb *redis.Client, cfg Config) func(http.Handler) http.Handler`.
- Use the client IP address (`r.RemoteAddr` or `X-Forwarded-For`) or user ID as the rate limit key.

### Phase 4: Integration
- Modify `cmd/server/main.go`
- Initialize the `redis.Client` using the loaded configuration.
- Attach the `ratelimit.RateLimit` middleware to the `chi` router or to specific endpoints.

## Verification Plan

### Automated Tests
- Run `task test` to execute all project tests.
- Add unit tests in `internal/middleware/ratelimit_test.go` using a mock Redis client (e.g., `github.com/go-redis/redismock/v9`) to verify the middleware returns 429 after the limit is reached.
- Run `golangci-lint run ./...` (or `task lint`) to verify code quality.

### Manual Verification
- Start the server using `task run` (and ensure Docker compose is running with Redis).
- Send rapid identical requests to an endpoint (e.g., using `curl`/`hey`/`ab`) to verify that the HTTP status `429 Too Many Requests` is returned correctly when the limit is breached.
- Check the server logs (powered by `slog`) to observe rate-limit rejections.

## Dependencies
- Redis (Docker service)
- `github.com/redis/go-redis/v9`

## Risks
- HIGH: Added latency from Redis calls for every HTTP request. (Mitigation: use Redis connection pooling).
- MEDIUM: Determining the correct client IP if behind a load balancer. (Mitigation: safely extract `X-Forwarded-For` or `X-Real-IP`).
- LOW: Redis unavailability. (Mitigation: implement a fail-open configuration to bypass rate limiting if Redis is down, avoiding a full API outage).

## Estimated Complexity: MEDIUM
- Backend implementation: 2-3 hours
- Infrastructure (Docker/Config): 1 hour
- Testing: 2-3 hours
- Total: 5-7 hours

**WAITING FOR CONFIRMATION**: Proceed with this plan? (yes/no/modify)
