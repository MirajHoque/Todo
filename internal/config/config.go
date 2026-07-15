// Package config loads all application configuration from environment variables.
// We never hardcode secrets. Every value has a clear default or panics loudly
// at startup if it's missing — fail fast, fail clearly.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds every runtime setting the app needs.
// Fields are grouped by concern so it's easy to see what each package uses.
type Config struct {
	// Server
	Host         string
	Port         string
	Env          string // "development" | "production"
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration

	// Database (Azure PostgreSQL)
	DatabaseURL   string
	DBMaxConns    int32
	DBMinConns    int32
	DBMaxConnIdle time.Duration

	// Auth
	JWTSecret  string
	JWTExpiry  time.Duration
	BCryptCost int

	// Anthropic (for agents)
	AnthropicAPIKey string
	AgentModel      string

	// Logging
	LogLevel  string // "debug" | "info" | "warn" | "error"
	LogFormat string // "json" | "text"
}

// Load reads configuration from environment variables.
// Call this once at startup. Any missing required value causes an immediate panic
// with a clear message — better to crash at boot than fail silently in production.
func Load() (*Config, error) {
	cfg := &Config{
		// Server defaults
		Host:         getEnv("HOST", "0.0.0.0"),
		Port:         getEnv("PORT", "8080"),
		Env:          getEnv("ENV", "development"),
		ReadTimeout:  getDuration("SERVER_READ_TIMEOUT", 10*time.Second),
		WriteTimeout: getDuration("SERVER_WRITE_TIMEOUT", 30*time.Second),
		IdleTimeout:  getDuration("SERVER_IDLE_TIMEOUT", 60*time.Second),

		// Database
		DatabaseURL:   requireEnv("DATABASE_URL"),
		DBMaxConns:    int32(getInt("DB_MAX_CONNS", 10)),
		DBMinConns:    int32(getInt("DB_MIN_CONNS", 2)),
		DBMaxConnIdle: getDuration("DB_MAX_CONN_IDLE", 5*time.Minute),

		// Auth
		JWTSecret:  requireEnv("JWT_SECRET"),
		JWTExpiry:  getDuration("JWT_EXPIRY", 24*time.Hour),
		BCryptCost: getInt("BCRYPT_COST", 12),

		// Anthropic
		AnthropicAPIKey: requireEnv("ANTHROPIC_API_KEY"),
		AgentModel:      getEnv("AGENT_MODEL", "claude-sonnet-4-20250514"),

		// Logging
		LogLevel:  getEnv("LOG_LEVEL", "info"),
		LogFormat: getEnv("LOG_FORMAT", "json"),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validate checks that config values are within acceptable ranges.
func (c *Config) validate() error {
	if c.Env != "development" && c.Env != "production" {
		return fmt.Errorf("config: ENV must be 'development' or 'production', got %q", c.Env)
	}
	if c.BCryptCost < 10 || c.BCryptCost > 31 {
		return fmt.Errorf("config: BCRYPT_COST must be between 10 and 31, got %d", c.BCryptCost)
	}
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("config: JWT_SECRET must be at least 32 characters")
	}
	return nil
}

// IsProduction returns true when running in production mode.
func (c *Config) IsProduction() bool {
	return c.Env == "production"
}

// Addr returns the full listen address for the HTTP server.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%s", c.Host, c.Port)
}

// ---- helpers ----------------------------------------------------------------

// requireEnv returns the value of an env var or panics with a helpful message.
func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("config: required environment variable %q is not set", key))
	}
	return v
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
