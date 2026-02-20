# Stage 1: Build the Go binary
# We use the official Go alpine image to get a clean build environment
FROM golang:1.22-alpine AS builder

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
RUN go mod download

# Copy the source from the current directory to the Working Directory inside the container
COPY . .

# Build the Go app
# CGO_ENABLED=0 creates a completely static binary
# -a forces a rebuild
# -installsuffix cgo helps with cgo-specific dependencies (though disabled here for safety)
# -ldflags="-w -s" strips debugging information to drastically reduce binary size
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o /app/telemetry-api ./cmd/server/main.go

# Stage 2: Create a minimal image
# We use 'scratch', an empty docker image. This guarantees zero vulnerabilities from OS packages!
FROM scratch

# Import the user and group files from the builder
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group

# Copy CA certificates to allow external SSL/HTTPS calls if our app ever needs them
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the Pre-built binary file from the previous stage
COPY --from=builder /app/telemetry-api /telemetry-api

# Expose port 8080 to the outside world
EXPOSE 8080

# Command to run the executable
ENTRYPOINT ["/telemetry-api"]
