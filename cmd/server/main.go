package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Yakov-Gorochovsky/project/internal/config"
	"github.com/Yakov-Gorochovsky/project/internal/handler"
	"github.com/Yakov-Gorochovsky/project/internal/repository"
	"github.com/Yakov-Gorochovsky/project/internal/server"
	"github.com/Yakov-Gorochovsky/project/internal/worker"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	slog.Info("Starting Telemetry API", "port", cfg.Port)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var repo repository.TelemetryRepository
	var pinger handler.Pinger

	if cfg.StorageBackend == "memory" || cfg.DatabaseURL == "memory" {
		slog.Info("Running with in-memory storage (zero-dependency local development mode)")
		memRepo := repository.NewMemoryTelemetryRepo()
		repo = memRepo
		pinger = memRepo
	} else {
		// Problem: Default pgxpool settings do not adapt to CPU cores/workers, risking connection starvation or exhaustion.
		// Solution: Calibrate connection pool dynamically to worker concurrency with warm minimum connections.
		poolConfig, err := repository.NewPoolConfig(cfg.DatabaseURL, cfg.WorkerCount)
		if err != nil {
			slog.Error("Failed to build database pool configuration", "error", err)
			os.Exit(1)
		}

		pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
		if err != nil {
			slog.Error("Unable to connect to database", "error", err)
			os.Exit(1)
		}
		defer pool.Close()

		if err := pool.Ping(ctx); err != nil {
			slog.Error("Cannot ping database", "error", err)
			os.Exit(1)
		}
		slog.Info("Database connection pool established")

		repo = repository.NewPostgresTelemetryRepo(pool)
		pinger = pool
	}

	flushTimeoutSec, _ := time.ParseDuration(cfg.FlushTimeout + "s")
	ingester := worker.NewIngester(repo, cfg.BatchSize, flushTimeoutSec, cfg.WorkerCount)
	slog.Info("Ingestion worker pool started", "workers", cfg.WorkerCount, "batch_size", cfg.BatchSize)

	telemetryHandler := handler.NewTelemetryHandler(ingester)
	// Problem: Single unsegmented /health probe causes cascading crashloop pod kills during DB timeouts.
	// Solution: Segregate liveness and readiness; wire pool pinger into /ready probe.
	router := handler.NewRouterWithPinger(telemetryHandler, pinger)

	// Problem: Default http.Server lacks ReadHeaderTimeout, leaving sockets vulnerable to Slowloris attacks.
	// Solution: Instantiate server via hardened server factory enforcing strict read header deadlines and header byte caps.
	srv := server.New(router, cfg.Port)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server crashed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	slog.Info("Shutting down server gracefully...")
	ctxShutDown, cancelShutDown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutDown()

	if err := srv.Shutdown(ctxShutDown); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}

	ingester.Stop()
	slog.Info("Server exited properly")
}
