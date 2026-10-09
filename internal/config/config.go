package config

import (
	"os"
	"runtime"
	"strconv"
)

// AppConfig holds all environment-level configuration for the application.
type AppConfig struct {
	Port           string
	DatabaseURL    string
	StorageBackend string
	BatchSize      int
	FlushTimeout   string
	WorkerCount    int
}

// Load reads from environment variables, providing sensible defaults if missing.
func Load() *AppConfig {
	defaultWorkers := runtime.NumCPU()
	if defaultWorkers < 2 {
		defaultWorkers = 2
	}

	return &AppConfig{
		Port:           getEnv("PORT", "8080"),
		DatabaseURL:    getEnv("DATABASE_URL", "postgres://user:password@localhost:5432/telemetry"),
		StorageBackend: getEnv("STORAGE_BACKEND", "postgres"),
		BatchSize:      getEnvAsInt("BATCH_SIZE", 500),
		FlushTimeout:   getEnv("FLUSH_TIMEOUT_SEC", "3"),
		WorkerCount:    getEnvAsInt("WORKER_COUNT", defaultWorkers),
	}
}

func getEnv(key string, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvAsInt(key string, fallback int) int {
	strValue := getEnv(key, "")
	if strValue == "" {
		return fallback
	}
	val, err := strconv.Atoi(strValue)
	if err != nil {
		return fallback
	}
	return val
}
