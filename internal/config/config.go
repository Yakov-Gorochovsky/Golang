package config

import (
	"os"
	"strconv"
)

// AppConfig holds all environment-level configuration for the application.
type AppConfig struct {
	Port         string
	DatabaseURL  string
	BatchSize    int
	FlushTimeout string // Time in seconds to wait before flushing buffer
}

// Load reads from environment variables, providing sensible defaults if missing.
func Load() *AppConfig {
	return &AppConfig{
		Port:         getEnv("PORT", "8080"),
		DatabaseURL:  getEnv("DATABASE_URL", "postgres://user:password@localhost:5432/telemetry"),
		BatchSize:    getEnvAsInt("BATCH_SIZE", 500),
		FlushTimeout: getEnv("FLUSH_TIMEOUT_SEC", "3"),
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
