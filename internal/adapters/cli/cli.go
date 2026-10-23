// Package cli is the console adapter: it parses argv and prints results. It
// depends on narrow interfaces it declares itself, never on concrete adapters.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/r0lm0/doodle-assistant/internal/app"
	"github.com/r0lm0/doodle-assistant/internal/eval"
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

// AgenticAsker answers with search-judge-reformulate retrieval.
type AgenticAsker interface {
	Ask(ctx context.Context, question string, emit func(delta string) error) (app.AgenticResult, error)
}

// AgentRunner runs the tool-calling loop.
type AgentRunner interface {
	Run(ctx context.Context, prompt string, emit func(delta string) error) (app.AgentResult, error)
}

// Evaluator measures retrieval quality against a dataset.
type Evaluator interface {
	Evaluate(ctx context.Context, questionsPath string, ks []int, verbose bool) (eval.Report, error)
}

// Asker answers questions, with or without retrieval.
type Asker interface {
	Ask(ctx context.Context, question string, emit func(delta string) error) (app.AskResult, error)
	AskWithoutContext(ctx context.Context, question string, emit func(delta string) error) (app.AskResult, error)
}

// Deps is everything the CLI needs. Out and Err make it testable without a
// terminal.
type Deps struct {
	Migrator  MigrationApplier
	Ingester  Ingester
	Asker     Asker
	Evaluator Evaluator
	Agent     AgentRunner
	Agentic   AgenticAsker
	Retriever eval.Retriever
	Handler   http.Handler
	HTTPAddr  string
	Stats     StatsReader
	Out       io.Writer
	Err       io.Writer
	Logger    *slog.Logger
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
  eval      measure recall@K of the retriever against a dataset
  agent     run the tool-calling agent over a simulated canvas
  serve     start the HTTP API and the UI
  stats     show how many documents and chunks are indexed
  help      show this message

Ask options:
  -no-context   answer without retrieval, to compare against RAG
  -agentic      search, judge the evidence and reformulate before answering

Eval options:
  -questions    dataset path (default eval/questions.json)
  -k            comma separated K values (default 1,3,5,8)
  -agentic      measure the agentic retriever instead of the single-shot one
  -verbose      print the rank of every question
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
	case "eval":
		return c.eval(ctx, args[1:])
	case "agent":
		return c.agent(ctx, args[1:])
	case "serve":
		return c.serve(ctx)
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
	agentic := flags.Bool("agentic", false, "search, judge and reformulate before answering")
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
	switch {
	case *agentic:
		if c.deps.Agentic == nil {
			fmt.Fprintln(c.deps.Err, "ask: no agentic asker configured")
			return exitFailure
		}
		agenticResult, agenticErr := c.deps.Agentic.Ask(ctx, question, stream)
		err = agenticErr
		// Judge time is retrieval work, not generation: fold it into search so the
		// printed line cannot suggest the model got faster.
		result = app.AskResult{
			Answer:       agenticResult.Answer,
			Retrieved:    agenticResult.Retrieved,
			EmbedTime:    agenticResult.EmbedTime,
			SearchTime:   agenticResult.SearchTime + agenticResult.JudgeTime,
			GenerateTime: agenticResult.GenerateTime,
		}
		if agenticErr == nil {
			fmt.Fprintln(c.deps.Out)
			fmt.Fprintf(c.deps.Out, "intentos: %d\n", agenticResult.Attempts)
			fmt.Fprintf(c.deps.Out, "consultas: %s\n", strings.Join(agenticResult.Queries, " | "))
			fmt.Fprintf(c.deps.Out, "juez: %s\n", agenticResult.JudgeReason)
		}
	case *noContext:
		result, err = c.deps.Asker.AskWithoutContext(ctx, question, stream)
	default:
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

// agent streams the final answer and prints what the model asked for, so the
// tool calls stay auditable instead of happening behind the answer.
// serve runs the HTTP API until the context is cancelled (Ctrl+C), then shuts
// down gracefully so in-flight answers are not cut mid-stream.
func (c *CLI) serve(ctx context.Context) int {
	if c.deps.Handler == nil {
		fmt.Fprintln(c.deps.Err, "serve: no handler configured")
		return exitFailure
	}

	addr := c.deps.HTTPAddr
	if addr == "" {
		addr = ":8080"
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           c.deps.Handler,
		ReadHeaderTimeout: 5 * time.Second,
		// No WriteTimeout on purpose: an SSE stream can last minutes.
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	fmt.Fprint(c.deps.Out, "escuchando en http://localhost")
	fmt.Fprintln(c.deps.Out, addr)

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprint(c.deps.Err, "serve: ")
		fmt.Fprintln(c.deps.Err, err)
		return exitFailure
	}
	return exitOK
}

func (c *CLI) agent(ctx context.Context, args []string) int {
	prompt := strings.TrimSpace(strings.Join(args, " "))
	if prompt == "" {
		fmt.Fprintln(c.deps.Err, "agent: missing prompt")
		return exitUsage
	}
	if c.deps.Agent == nil {
		fmt.Fprintln(c.deps.Err, "agent: no agent configured")
		return exitFailure
	}

	stream := func(delta string) error {
		_, err := fmt.Fprint(c.deps.Out, delta)
		return err
	}

	result, err := c.deps.Agent.Run(ctx, prompt, stream)
	if err != nil {
		fmt.Fprintln(c.deps.Err)
		fmt.Fprintf(c.deps.Err, "agent: %v\n", err)
		return exitFailure
	}
	fmt.Fprintln(c.deps.Out)

	if len(result.Tools) == 0 {
		fmt.Fprintln(c.deps.Out, "---")
		fmt.Fprintln(c.deps.Out, "herramientas: ninguna (el modelo respondió sin tools)")
		return exitOK
	}

	fmt.Fprintln(c.deps.Out, "---")
	fmt.Fprintf(c.deps.Out, "herramientas (%d):\n", len(result.Tools))
	for _, trace := range result.Tools {
		outcome := trace.Result
		if trace.Error != "" {
			outcome = "ERROR: " + trace.Error
		}
		fmt.Fprintf(c.deps.Out, "  [%d] %s(%s) -> %s\n", trace.Iteration, trace.Name, trace.Arguments, outcome)
	}
	fmt.Fprintf(c.deps.Out, "iteraciones: %d\n", result.Iterations)
	return exitOK
}

func (c *CLI) eval(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("eval", flag.ContinueOnError)
	flags.SetOutput(c.deps.Err)
	questionsPath := flags.String("questions", "eval/questions.json", "path to the evaluation dataset")
	ksFlag := flags.String("k", "1,3,5,8", "comma separated K values")
	verbose := flags.Bool("verbose", false, "print the rank of each question")
	agentic := flags.Bool("agentic", false, "measure the agentic retriever")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}

	ks, err := parseKs(*ksFlag)
	if err != nil {
		fmt.Fprintf(c.deps.Err, "eval: %v\n", err)
		return exitUsage
	}
	if c.deps.Evaluator == nil {
		fmt.Fprintln(c.deps.Err, "eval: no evaluator configured")
		return exitFailure
	}

	var report eval.Report
	if *agentic {
		runner, ok := c.deps.Evaluator.(interface {
			EvaluateWith(ctx context.Context, path string, ks []int, verbose bool, retriever eval.Retriever) (eval.Report, error)
		})
		if !ok || c.deps.Retriever == nil {
			fmt.Fprintln(c.deps.Err, "eval: agentic evaluation is not configured")
			return exitFailure
		}
		report, err = runner.EvaluateWith(ctx, *questionsPath, ks, *verbose, c.deps.Retriever)
	} else {
		report, err = c.deps.Evaluator.Evaluate(ctx, *questionsPath, ks, *verbose)
	}
	if err != nil {
		fmt.Fprintf(c.deps.Err, "eval: %v\n", err)
		return exitFailure
	}

	fmt.Fprint(c.deps.Out, report.String())
	return exitOK
}

func parseKs(raw string) ([]int, error) {
	parts := strings.Split(raw, ",")
	ks := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("K must be an integer, got %q", part)
		}
		if k <= 0 {
			return nil, fmt.Errorf("K must be positive, got %d", k)
		}
		ks = append(ks, k)
	}
	if len(ks) == 0 {
		return nil, errors.New("at least one K value is required")
	}
	return ks, nil
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
