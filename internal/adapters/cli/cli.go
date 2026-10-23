// Package cli is the console adapter: it parses argv and prints results. It
// depends on narrow interfaces it declares itself, never on concrete adapters.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/r0lm0/doodle-assistant/internal/app"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

// MigrationApplier applies pending schema migrations.
type MigrationApplier interface {
	Apply(ctx context.Context) ([]string, error)
}

// Ingester indexes the knowledge base.
type Ingester interface {
	Ingest(ctx context.Context) (app.IngestStats, error)
}

// StatsReader reports how much is indexed.
type StatsReader interface {
	Stats(ctx context.Context) (ports.StoreStats, error)
}

// Asker answers questions, with or without retrieval.
type Asker interface {
	Ask(ctx context.Context, question string, emit func(delta string) error) (app.AskResult, error)
	AskWithoutContext(ctx context.Context, question string, emit func(delta string) error) (app.AskResult, error)
}

// Deps is everything the CLI needs. Out and Err make it testable without a
// terminal.
type Deps struct {
	Migrator MigrationApplier
	Ingester Ingester
	Asker    Asker
	Stats    StatsReader
	Out      io.Writer
	Err      io.Writer
	Logger   *slog.Logger
}

// CLI dispatches subcommands and returns a process exit code.
type CLI struct {
	deps Deps
}

const usage = `doodle-assistant - RAG + tool calling assistant

Usage:
  doodle-assistant <command>

Commands:
  migrate   apply pending SQL migrations
  ingest    chunk, embed and store the markdown knowledge base
  ask       answer a question from the knowledge base (streaming)
  stats     show how many documents and chunks are indexed
  help      show this message

Ask options:
  -no-context   answer without retrieval, to compare against RAG
`

// Exit codes: 0 success, 1 runtime failure, 2 usage error.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

func New(deps Deps) (*CLI, error) {
	if deps.Out == nil {
		deps.Out = io.Discard
	}
	if deps.Err == nil {
		deps.Err = io.Discard
	}
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &CLI{deps: deps}, nil
}

// Run executes one command.
func (c *CLI) Run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(c.deps.Err, usage)
		return exitUsage
	}

	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(c.deps.Out, usage)
		return exitOK
	case "migrate":
		return c.migrate(ctx)
	case "ingest":
		return c.ingest(ctx)
	case "ask":
		return c.ask(ctx, args[1:])
	case "stats":
		return c.stats(ctx)
	default:
		fmt.Fprintf(c.deps.Err, "unknown command %q\n\n%s", args[0], usage)
		return exitUsage
	}
}

func (c *CLI) migrate(ctx context.Context) int {
	if c.deps.Migrator == nil {
		fmt.Fprintln(c.deps.Err, "migrate: no migrator configured")
		return exitFailure
	}

	applied, err := c.deps.Migrator.Apply(ctx)
	if err != nil {
		fmt.Fprintf(c.deps.Err, "migrate: %v\n", err)
		return exitFailure
	}

	if len(applied) == 0 {
		fmt.Fprintln(c.deps.Out, "migrations: nothing to apply")
		return exitOK
	}
	for _, name := range applied {
		fmt.Fprintf(c.deps.Out, "migrations: applied %s\n", name)
	}
	return exitOK
}

func (c *CLI) ingest(ctx context.Context) int {
	if c.deps.Ingester == nil {
		fmt.Fprintln(c.deps.Err, "ingest: no ingester configured")
		return exitFailure
	}

	start := time.Now()
	stats, err := c.deps.Ingester.Ingest(ctx)
	if err != nil {
		fmt.Fprintf(c.deps.Err, "ingest: %v\n", err)
		return exitFailure
	}

	fmt.Fprintf(c.deps.Out,
		"scanned=%d skipped=%d indexed=%d chunks=%d embed_calls=%d\nembed=%s store=%s total=%s\n",
		stats.Scanned, stats.Skipped, stats.Indexed, stats.Chunks, stats.EmbedCalls,
		stats.EmbedTime.Round(time.Millisecond), stats.StoreTime.Round(time.Millisecond),
		time.Since(start).Round(time.Millisecond),
	)
	return exitOK
}

// ask streams the answer to stdout and prints the sources afterwards, so the user
// sees the evidence even if the model forgot to cite it inline.
func (c *CLI) ask(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("ask", flag.ContinueOnError)
	flags.SetOutput(c.deps.Err)
	noContext := flags.Bool("no-context", false, "answer without retrieval (control group)")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}

	question := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if question == "" {
		fmt.Fprintln(c.deps.Err, "ask: missing question")
		return exitUsage
	}
	if c.deps.Asker == nil {
		fmt.Fprintln(c.deps.Err, "ask: no asker configured")
		return exitFailure
	}

	stream := func(delta string) error {
		_, err := fmt.Fprint(c.deps.Out, delta)
		return err
	}

	var (
		result app.AskResult
		err    error
	)
	if *noContext {
		result, err = c.deps.Asker.AskWithoutContext(ctx, question, stream)
	} else {
		result, err = c.deps.Asker.Ask(ctx, question, stream)
	}
	if err != nil {
		fmt.Fprintf(c.deps.Err, "\nask: %v\n", err)
		return exitFailure
	}
	fmt.Fprintln(c.deps.Out)

	if len(result.Answer.Sources) > 0 {
		fmt.Fprintln(c.deps.Out, "---\nfuentes:")
		for i, source := range result.Answer.Sources {
			fmt.Fprintf(c.deps.Out, "  [%d] %.4f  %s\n", i+1, source.Distance, source.Citation())
		}
	}
	fmt.Fprintf(c.deps.Out, "tiempos: embed=%s search=%s generate=%s retrieved=%d\n",
		result.EmbedTime.Round(time.Millisecond),
		result.SearchTime.Round(time.Millisecond),
		result.GenerateTime.Round(time.Millisecond),
		result.Retrieved,
	)
	return exitOK
}

func (c *CLI) stats(ctx context.Context) int {
	if c.deps.Stats == nil {
		fmt.Fprintln(c.deps.Err, "stats: no store configured")
		return exitFailure
	}

	stats, err := c.deps.Stats.Stats(ctx)
	if err != nil {
		fmt.Fprintf(c.deps.Err, "stats: %v\n", err)
		return exitFailure
	}

	fmt.Fprintf(c.deps.Out, "documents=%d chunks=%d\n", stats.Documents, stats.Chunks)
	return exitOK
}
