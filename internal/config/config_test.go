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
}

func TestLoadReadsOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://neon.example/db")
	t.Setenv("EMBED_MODEL", "mxbai-embed-large")
	t.Setenv("EMBED_DIM", "1024")
	t.Setenv("EMBED_BATCH_SIZE", "8")
	t.Setenv("DOCS_DIR", "/srv/docs")
	t.Setenv("LOG_LEVEL", "debug")

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
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := map[string]string{
		"non-integer EMBED_DIM": "EMBED_DIM",
		"non-integer batch":     "EMBED_BATCH_SIZE",
		"unknown log level":     "LOG_LEVEL",
		"zero dimension":        "EMBED_DIM_ZERO",
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
			}

			if _, err := config.Load(); err == nil {
				t.Fatalf("expected an error for %s", key)
			}
		})
	}
}
