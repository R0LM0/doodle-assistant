package cli_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/r0lm0/doodle-assistant/internal/adapters/cli"
	"github.com/r0lm0/doodle-assistant/internal/app"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

type fakeMigrator struct {
	applied []string
	err     error
}

func (f *fakeMigrator) Apply(context.Context) ([]string, error) { return f.applied, f.err }

type fakeIngester struct {
	stats app.IngestStats
	err   error
}

func (f *fakeIngester) Ingest(context.Context) (app.IngestStats, error) { return f.stats, f.err }

type fakeStats struct {
	stats ports.StoreStats
	err   error
}

func (f *fakeStats) Stats(context.Context) (ports.StoreStats, error) { return f.stats, f.err }

func newCLI(t *testing.T, deps cli.Deps) (*cli.CLI, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	deps.Out, deps.Err = out, errOut

	c, err := cli.New(deps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, out, errOut
}

func TestRunWithoutArgumentsPrintsUsageAndFails(t *testing.T) {
	c, _, errOut := newCLI(t, cli.Deps{})

	if code := c.Run(context.Background(), nil); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "Usage:") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestRunUnknownCommandFails(t *testing.T) {
	c, _, errOut := newCLI(t, cli.Deps{})

	if code := c.Run(context.Background(), []string{"hacer-cafe"}); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), `unknown command "hacer-cafe"`) {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestMigrateReportsAppliedFiles(t *testing.T) {
	c, out, _ := newCLI(t, cli.Deps{Migrator: &fakeMigrator{applied: []string{"0001_init.sql", "0002_hnsw.sql"}}})

	if code := c.Run(context.Background(), []string{"migrate"}); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if got := out.String(); !strings.Contains(got, "0001_init.sql") || !strings.Contains(got, "0002_hnsw.sql") {
		t.Errorf("stdout = %q", got)
	}
}

func TestMigrateReportsNothingToApply(t *testing.T) {
	c, out, _ := newCLI(t, cli.Deps{Migrator: &fakeMigrator{}})

	if code := c.Run(context.Background(), []string{"migrate"}); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out.String(), "nothing to apply") {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestIngestPrintsStageTimings(t *testing.T) {
	c, out, _ := newCLI(t, cli.Deps{Ingester: &fakeIngester{stats: app.IngestStats{
		Scanned: 8, Skipped: 1, Indexed: 7, Chunks: 63, EmbedCalls: 3,
		EmbedTime: 1500 * time.Millisecond, StoreTime: 40 * time.Millisecond,
	}}})

	if code := c.Run(context.Background(), []string{"ingest"}); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	got := out.String()
	for _, want := range []string{"scanned=8", "skipped=1", "indexed=7", "chunks=63", "embed_calls=3", "embed=1.5s", "store=40ms"} {
		if !strings.Contains(got, want) {
			t.Errorf("stdout missing %q:\n%s", want, got)
		}
	}
}

func TestCommandErrorsReturnExitCodeOne(t *testing.T) {
	cause := errors.New("database exploded")

	c, _, errOut := newCLI(t, cli.Deps{Ingester: &fakeIngester{err: cause}})
	if code := c.Run(context.Background(), []string{"ingest"}); code != 1 {
		t.Fatalf("ingest exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "database exploded") {
		t.Errorf("stderr = %q", errOut.String())
	}

	c, _, _ = newCLI(t, cli.Deps{Migrator: &fakeMigrator{err: cause}})
	if code := c.Run(context.Background(), []string{"migrate"}); code != 1 {
		t.Fatalf("migrate exit code = %d, want 1", code)
	}

	c, _, _ = newCLI(t, cli.Deps{Stats: &fakeStats{err: cause}})
	if code := c.Run(context.Background(), []string{"stats"}); code != 1 {
		t.Fatalf("stats exit code = %d, want 1", code)
	}
}

func TestStatsAndHelp(t *testing.T) {
	c, out, _ := newCLI(t, cli.Deps{Stats: &fakeStats{stats: ports.StoreStats{Documents: 8, Chunks: 63}}})
	if code := c.Run(context.Background(), []string{"stats"}); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out.String(), "documents=8 chunks=63") {
		t.Errorf("stdout = %q", out.String())
	}

	c, out, _ = newCLI(t, cli.Deps{})
	if code := c.Run(context.Background(), []string{"help"}); code != 0 {
		t.Fatalf("help exit code = %d", code)
	}
	if !strings.Contains(out.String(), "ingest") {
		t.Errorf("help output = %q", out.String())
	}
}
