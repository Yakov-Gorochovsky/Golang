# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies and root CA certificates
RUN apk add --no-cache ca-certificates tzdata

# Cache Go module dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy full source tree
COPY . .

# Compile statically linked binary without debug symbols
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/telemetry-api ./cmd/server/main.go

# Production runtime stage: minimal scratch container
FROM scratch

# Copy root certificates and timezone data
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /app/telemetry-api /telemetry-api

# Run as non-root user (nobody:nogroup) for container security compliance
USER 65534:65534

EXPOSE 8080

ENTRYPOINT ["/telemetry-api"]
