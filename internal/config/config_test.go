package config_test

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/r0lm0/doodle-assistant/internal/config"
)

func clearEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"DATABASE_URL", "OLLAMA_BASE_URL", "CHAT_MODEL", "EMBED_MODEL",
		"EMBED_DIM", "EMBED_BATCH_SIZE", "DOCS_DIR", "LOG_LEVEL",
		"SEARCH_MODE", "RRF_K", "TOP_K", "API_TOKEN",
		"RATE_LIMIT_RPS", "RATE_LIMIT_BURST",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadUsesDevelopmentDefaults(t *testing.T) {
	clearEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !strings.Contains(cfg.DatabaseURL, "localhost:5434") {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.OllamaBaseURL != "http://localhost:11434" {
		t.Errorf("OllamaBaseURL = %q", cfg.OllamaBaseURL)
	}
	if cfg.EmbedModel != "embeddinggemma" || cfg.EmbedDim != 768 {
		t.Errorf("embedding defaults = %q/%d", cfg.EmbedModel, cfg.EmbedDim)
	}
	if cfg.ChatModel != "gemma-4-e4b" {
		t.Errorf("ChatModel = %q", cfg.ChatModel)
	}
	if cfg.EmbedBatchSize != 32 {
		t.Errorf("EmbedBatchSize = %d", cfg.EmbedBatchSize)
	}
	if cfg.DocsDir != "docs" {
		t.Errorf("DocsDir = %q", cfg.DocsDir)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v", cfg.LogLevel)
	}
	if cfg.SearchMode != "vector" {
		t.Errorf("SearchMode = %q", string(cfg.SearchMode))
	}
	if cfg.RRFK != 60 {
		t.Errorf("RRFK = %v", cfg.RRFK)
	}
	if cfg.HTTPAddr != "127.0.0.1:8080" {
		t.Errorf("HTTPAddr = %q, want loopback default", cfg.HTTPAddr)
	}
	if cfg.APIToken != "" {
		t.Errorf("APIToken = %q, want empty default", cfg.APIToken)
	}
	if cfg.RateLimitRPS != 1 || cfg.RateLimitBurst != 5 {
		t.Errorf("rate limit = %v/%d", cfg.RateLimitRPS, cfg.RateLimitBurst)
	}
}

func TestLoadReadsOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://neon.example/db")
	t.Setenv("EMBED_MODEL", "mxbai-embed-large")
	t.Setenv("EMBED_DIM", "1024")
	t.Setenv("EMBED_BATCH_SIZE", "8")
	t.Setenv("DOCS_DIR", "/srv/docs")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("SEARCH_MODE", "hybrid")
	t.Setenv("RRF_K", "30")
	t.Setenv("API_TOKEN", "secreto")
	t.Setenv("RATE_LIMIT_RPS", "2")
	t.Setenv("RATE_LIMIT_BURST", "10")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.DatabaseURL != "postgres://neon.example/db" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.EmbedModel != "mxbai-embed-large" || cfg.EmbedDim != 1024 {
		t.Errorf("embedding = %q/%d", cfg.EmbedModel, cfg.EmbedDim)
	}
	if cfg.EmbedBatchSize != 8 || cfg.DocsDir != "/srv/docs" {
		t.Errorf("batch/docs = %d/%q", cfg.EmbedBatchSize, cfg.DocsDir)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v", cfg.LogLevel)
	}
	if cfg.SearchMode != "hybrid" || cfg.RRFK != 30 {
		t.Errorf("search = %q/%v", string(cfg.SearchMode), cfg.RRFK)
	}
	if cfg.APIToken != "secreto" || cfg.RateLimitRPS != 2 || cfg.RateLimitBurst != 10 {
		t.Errorf("auth/limit = %q/%v/%d", cfg.APIToken, cfg.RateLimitRPS, cfg.RateLimitBurst)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := map[string]string{
		"non-integer EMBED_DIM": "EMBED_DIM",
		"non-integer batch":     "EMBED_BATCH_SIZE",
		"unknown log level":     "LOG_LEVEL",
		"zero dimension":        "EMBED_DIM_ZERO",
		"unknown search mode":   "SEARCH_MODE",
		"non-numeric RRF_K":     "RRF_K",
		"zero RRF_K":            "RRF_K_ZERO",
		"zero rate":             "RATE_LIMIT_RPS",
		"zero burst":            "RATE_LIMIT_BURST",
	}

	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			clearEnv(t)
			switch key {
			case "EMBED_DIM", "EMBED_BATCH_SIZE":
				t.Setenv(key, "muchos")
			case "LOG_LEVEL":
				t.Setenv(key, "verbose")
			case "EMBED_DIM_ZERO":
				t.Setenv("EMBED_DIM", "0")
			case "SEARCH_MODE":
				t.Setenv("SEARCH_MODE", "bm25")
			case "RRF_K":
				t.Setenv("RRF_K", "mucho")
			case "RRF_K_ZERO":
				t.Setenv("RRF_K", "0")
			case "RATE_LIMIT_RPS":
				t.Setenv("RATE_LIMIT_RPS", "0")
			case "RATE_LIMIT_BURST":
				t.Setenv("RATE_LIMIT_BURST", "0")
			}

			if _, err := config.Load(); err == nil {
				t.Fatalf("expected an error for %s", key)
			}
		})
	}
}
