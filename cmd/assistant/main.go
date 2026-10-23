// Command assistant is the composition root: the only place that knows every
// adapter and wires them to the ports the application needs.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/r0lm0/doodle-assistant/internal/adapters/cli"
	"github.com/r0lm0/doodle-assistant/internal/adapters/filesystem"
	"github.com/r0lm0/doodle-assistant/internal/adapters/ollama"
	"github.com/r0lm0/doodle-assistant/internal/adapters/postgres"
	"github.com/r0lm0/doodle-assistant/internal/app"
	"github.com/r0lm0/doodle-assistant/internal/config"
	"github.com/r0lm0/doodle-assistant/migrations"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))

	// Ctrl+C or SIGTERM cancels the context that every call chains from, so a
	// long ingestion stops between documents instead of dying mid-transaction.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	poolCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(poolCtx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connect to postgres", "error", err)
		return 1
	}
	defer pool.Close()

	if err := pool.Ping(poolCtx); err != nil {
		logger.Error("ping postgres", "error", err, "hint", "is the pgvector container running?")
		return 1
	}

	store, err := postgres.NewStore(pool)
	if err != nil {
		logger.Error("build postgres store", "error", err)
		return 1
	}

	migrator, err := postgres.NewMigrator(pool, migrations.FS)
	if err != nil {
		logger.Error("build migrator", "error", err)
		return 1
	}

	embedder, err := ollama.NewEmbedder(ollama.EmbedderConfig{
		BaseURL: cfg.OllamaBaseURL,
		Model:   cfg.EmbedModel,
	})
	if err != nil {
		logger.Error("build ollama embedder", "error", err)
		return 1
	}

	loader, err := filesystem.NewLoader(cfg.DocsDir)
	if err != nil {
		logger.Error("build documents loader", "error", err)
		return 1
	}

	ingester, err := app.NewIngestDocs(loader, embedder, store, app.IngestConfig{
		EmbedBatchSize: cfg.EmbedBatchSize,
		Logger:         logger,
	})
	if err != nil {
		logger.Error("build ingest use case", "error", err)
		return 1
	}

	cmd, err := cli.New(cli.Deps{
		Migrator: migrator,
		Ingester: ingester,
		Stats:    store,
		Out:      os.Stdout,
		Err:      os.Stderr,
		Logger:   logger,
	})
	if err != nil {
		logger.Error("build cli", "error", err)
		return 1
	}

	return cmd.Run(ctx, os.Args[1:])
}
