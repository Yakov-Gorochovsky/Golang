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
	"github.com/Yakov-Gorochovsky/project/internal/worker"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	slog.Info("Starting Telemetry API", "port", cfg.Port)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
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

	repo := repository.NewPostgresTelemetryRepo(pool)

	flushTimeoutSec, _ := time.ParseDuration(cfg.FlushTimeout + "s")
	ingester := worker.NewIngester(repo, cfg.BatchSize, flushTimeoutSec, cfg.WorkerCount)
	slog.Info("Ingestion worker pool started", "workers", cfg.WorkerCount, "batch_size", cfg.BatchSize)

	telemetryHandler := handler.NewTelemetryHandler(ingester)
	router := handler.NewRouter(telemetryHandler)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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

	if err := server.Shutdown(ctxShutDown); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}

	ingester.Stop()
	slog.Info("Server exited properly")
}
