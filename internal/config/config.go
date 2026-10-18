// Package config loads the runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"
)

// LookupFunc has the signature of os.LookupEnv.
type LookupFunc func(key string) (string, bool)

// API is the configuration of memory-api.
type API struct {
	HTTPAddr            string
	MetricsAddr         string
	DatabaseURL         string
	DBMaxConns          int32
	EmbeddingDimensions int
	KeyCacheTTL         time.Duration
	KeyNegativeCacheTTL time.Duration
	MaxBodyBytes        int64
	LogLevel            slog.Level
	LogFormat           string
	Environment         string
	ShutdownTimeout     time.Duration
	ServiceName         string
	ServiceVersion      string
	TracesExporter      string
}

// LoadAPI reads the API configuration, applying defaults.
func LoadAPI(lookup LookupFunc) (API, error) {
	l := loader{lookup: lookup}
	cfg := API{
		HTTPAddr:            l.str("MEMORY_HTTP_ADDR", ":8080"),
		MetricsAddr:         l.str("MEMORY_METRICS_ADDR", ":9090"),
		DatabaseURL:         l.str("MEMORY_DATABASE_URL", ""),
		DBMaxConns:          int32(l.integer("MEMORY_DB_MAX_CONNS", 20, 1, 1000)),
		EmbeddingDimensions: l.integer("MEMORY_EMBEDDING_DIMENSIONS", 1536, 1, 16000),
		KeyCacheTTL:         l.duration("MEMORY_KEY_CACHE_TTL", 60*time.Second),
		KeyNegativeCacheTTL: l.duration("MEMORY_KEY_NEGATIVE_CACHE_TTL", 5*time.Second),
		MaxBodyBytes:        int64(l.integer("MEMORY_MAX_BODY_BYTES", 1<<20, 1, 1<<30)),
		LogFormat:           l.str("MEMORY_LOG_FORMAT", "json"),
		Environment:         l.str("MEMORY_ENVIRONMENT", "development"),
		ShutdownTimeout:     l.duration("MEMORY_SHUTDOWN_TIMEOUT", 15*time.Second),
		ServiceName:         l.str("OTEL_SERVICE_NAME", "memory-api"),
		ServiceVersion:      "dev",
		TracesExporter:      l.str("OTEL_TRACES_EXPORTER", "otlp"),
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(l.str("MEMORY_LOG_LEVEL", "info"))); err != nil {
		l.errs = append(l.errs, fmt.Errorf("MEMORY_LOG_LEVEL: %w", err))
	}
	if cfg.DatabaseURL == "" {
		l.errs = append(l.errs, errors.New("MEMORY_DATABASE_URL is required"))
	}
	if cfg.LogFormat != "json" && cfg.LogFormat != "text" {
		l.errs = append(l.errs, fmt.Errorf("MEMORY_LOG_FORMAT: must be json or text, got %q", cfg.LogFormat))
	}
	if cfg.TracesExporter != "otlp" && cfg.TracesExporter != "none" {
		l.errs = append(l.errs, fmt.Errorf("OTEL_TRACES_EXPORTER: must be otlp or none, got %q", cfg.TracesExporter))
	}
	return cfg, errors.Join(l.errs...)
}

// Admin is the configuration of memory-admin. Migrations and administrative
// commands use different database roles.
type Admin struct {
	MigratorDatabaseURL string
	AdminDatabaseURL    string
}

// LoadAdmin reads the memory-admin configuration.
func LoadAdmin(lookup LookupFunc) Admin {
	l := loader{lookup: lookup}
	return Admin{
		MigratorDatabaseURL: l.str("MEMORY_MIGRATOR_DATABASE_URL", ""),
		AdminDatabaseURL:    l.str("MEMORY_ADMIN_DATABASE_URL", ""),
	}
}

type loader struct {
	lookup LookupFunc
	errs   []error
}

func (l *loader) str(key, def string) string {
	if v, ok := l.lookup(key); ok && v != "" {
		return v
	}
	return def
}

func (l *loader) integer(key string, def, minimum, maximum int) int {
	v, ok := l.lookup(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < minimum || n > maximum {
		l.errs = append(l.errs, fmt.Errorf("%s: must be an integer between %d and %d, got %q", key, minimum, maximum, v))
		return def
	}
	return n
}

func (l *loader) duration(key string, def time.Duration) time.Duration {
	v, ok := l.lookup(key)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		l.errs = append(l.errs, fmt.Errorf("%s: must be a positive duration, got %q", key, v))
		return def
	}
	return d
}
