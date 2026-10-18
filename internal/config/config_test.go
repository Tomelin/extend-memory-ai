package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) LookupFunc {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestLoadAPIDefaults(t *testing.T) {
	cfg, err := LoadAPI(env(map[string]string{"MEMORY_DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.MetricsAddr != ":9090" || cfg.DBMaxConns != 20 ||
		cfg.EmbeddingDimensions != 1536 || cfg.KeyCacheTTL != time.Minute ||
		cfg.MaxBodyBytes != 1<<20 || cfg.LogLevel != slog.LevelInfo || cfg.LogFormat != "json" ||
		cfg.ServiceName != "memory-api" || cfg.TracesExporter != "otlp" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadAPIErrors(t *testing.T) {
	_, err := LoadAPI(env(map[string]string{
		"MEMORY_DB_MAX_CONNS":  "zero",
		"MEMORY_KEY_CACHE_TTL": "-1s",
		"MEMORY_LOG_FORMAT":    "xml",
		"MEMORY_LOG_LEVEL":     "loud",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"MEMORY_DATABASE_URL", "MEMORY_DB_MAX_CONNS", "MEMORY_KEY_CACHE_TTL", "MEMORY_LOG_FORMAT", "MEMORY_LOG_LEVEL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}
