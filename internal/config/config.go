// Package config loads the environment-only configuration. It is part of the
// composition layer: the domain never sees an environment variable.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// Development defaults mirror docker-compose.yml. They are not secrets: they are
// the throwaway local credentials of the compose service.
const (
	defaultDatabaseURL    = "postgres://doodle:doodle@localhost:5434/doodle?sslmode=disable"
	defaultOllamaBaseURL  = "http://localhost:11434"
	defaultChatModel      = "gemma-4-e4b"
	defaultEmbedModel     = "embeddinggemma"
	defaultEmbedDim       = 768
	defaultEmbedBatchSize = 32
	defaultTopK           = 4
	defaultChunkMaxChars  = 1200
	defaultChunkOverlap   = 150
	defaultChunkMinChars  = 200
	defaultDocsDir        = "docs"
	defaultHTTPAddr       = ":8080"
	defaultWebDir         = "web/dist"
	defaultLogLevel       = "info"
)

// Config is everything the binary needs to run.
type Config struct {
	DatabaseURL    string
	OllamaBaseURL  string
	ChatModel      string
	EmbedModel     string
	EmbedDim       int
	EmbedBatchSize int
	TopK           int
	ChunkMaxChars  int
	ChunkOverlap   int
	ChunkMinChars  int
	DocsDir        string
	HTTPAddr       string
	WebDir         string
	LogLevel       slog.Level
}

// Load reads the environment and fails on values that cannot work. Defaults are
// the local development stack, so `go run ./cmd/assistant ingest` works out of
// the box; a deployment overrides DATABASE_URL and OLLAMA_BASE_URL.
func Load() (Config, error) {
	embedDim, err := intEnv("EMBED_DIM", defaultEmbedDim)
	if err != nil {
		return Config{}, err
	}
	batchSize, err := intEnv("EMBED_BATCH_SIZE", defaultEmbedBatchSize)
	if err != nil {
		return Config{}, err
	}
	topK, err := intEnv("TOP_K", defaultTopK)
	if err != nil {
		return Config{}, err
	}
	chunkMax, err := intEnv("CHUNK_MAX_CHARS", defaultChunkMaxChars)
	if err != nil {
		return Config{}, err
	}
	chunkOverlap, err := intEnv("CHUNK_OVERLAP_CHARS", defaultChunkOverlap)
	if err != nil {
		return Config{}, err
	}
	chunkMin, err := intEnv("CHUNK_MIN_CHARS", defaultChunkMinChars)
	if err != nil {
		return Config{}, err
	}
	level, err := parseLevel(env("LOG_LEVEL", defaultLogLevel))
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		DatabaseURL:    env("DATABASE_URL", defaultDatabaseURL),
		OllamaBaseURL:  env("OLLAMA_BASE_URL", defaultOllamaBaseURL),
		ChatModel:      env("CHAT_MODEL", defaultChatModel),
		EmbedModel:     env("EMBED_MODEL", defaultEmbedModel),
		EmbedDim:       embedDim,
		EmbedBatchSize: batchSize,
		TopK:           topK,
		ChunkMaxChars:  chunkMax,
		ChunkOverlap:   chunkOverlap,
		ChunkMinChars:  chunkMin,
		DocsDir:        env("DOCS_DIR", defaultDocsDir),
		HTTPAddr:       env("HTTP_ADDR", defaultHTTPAddr),
		WebDir:         env("WEB_DIR", defaultWebDir),
		LogLevel:       level,
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return errors.New("config: DATABASE_URL must not be empty")
	}
	if strings.TrimSpace(c.HTTPAddr) == "" {
		return errors.New("config: HTTP_ADDR must not be empty")
	}
	if strings.TrimSpace(c.OllamaBaseURL) == "" {
		return errors.New("config: OLLAMA_BASE_URL must not be empty")
	}
	if strings.TrimSpace(c.EmbedModel) == "" {
		return errors.New("config: EMBED_MODEL must not be empty")
	}
	if c.EmbedDim <= 0 {
		return fmt.Errorf("config: EMBED_DIM must be positive, got %d", c.EmbedDim)
	}
	if c.EmbedBatchSize <= 0 {
		return fmt.Errorf("config: EMBED_BATCH_SIZE must be positive, got %d", c.EmbedBatchSize)
	}
	if c.ChunkMaxChars <= 0 {
		return fmt.Errorf("config: CHUNK_MAX_CHARS must be positive, got %d", c.ChunkMaxChars)
	}
	if c.ChunkOverlap < 0 || c.ChunkOverlap >= c.ChunkMaxChars {
		return fmt.Errorf("config: CHUNK_OVERLAP_CHARS must be between 0 and CHUNK_MAX_CHARS, got %d", c.ChunkOverlap)
	}
	if c.ChunkMinChars < 0 {
		return fmt.Errorf("config: CHUNK_MIN_CHARS must not be negative, got %d", c.ChunkMinChars)
	}
	if c.TopK <= 0 {
		return fmt.Errorf("config: TOP_K must be positive, got %d", c.TopK)
	}
	return nil
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func intEnv(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be an integer, got %q", key, raw)
	}
	return value, nil
}

func parseLevel(raw string) (slog.Level, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(raw)); err != nil {
		return 0, fmt.Errorf("config: LOG_LEVEL must be debug, info, warn or error, got %q", raw)
	}
	return level, nil
}
