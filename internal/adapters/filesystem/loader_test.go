package filesystem_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/r0lm0/doodle-assistant/internal/adapters/filesystem"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoadReadsMarkdownRecursivelyInOrder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "brushes.md", "# Pinceles\n\ncontenido\n")
	writeFile(t, dir, "sub/layers.md", "sin encabezado\n")
	writeFile(t, dir, "notes.txt", "no es markdown\n")

	loader, err := filesystem.NewLoader(dir)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	docs, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(docs) != 2 {
		t.Fatalf("got %d documents, want 2: %+v", len(docs), docs)
	}
	if docs[0].SourcePath != "brushes.md" || docs[0].Title != "Pinceles" {
		t.Errorf("doc 0 = %+v", docs[0])
	}
	if docs[1].SourcePath != "sub/layers.md" || docs[1].Title != "layers" {
		t.Errorf("doc 1 = %+v (want slash-separated path and filename title)", docs[1])
	}
	if docs[0].Content != "# Pinceles\n\ncontenido\n" {
		t.Errorf("content not preserved: %q", docs[0].Content)
	}
}

func TestLoadFailsOnEmptyDirectory(t *testing.T) {
	loader, err := filesystem.NewLoader(t.TempDir())
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	_, err = loader.Load(context.Background())
	if !errors.Is(err, filesystem.ErrNoDocuments) {
		t.Fatalf("err = %v, want ErrNoDocuments", err)
	}
}

func TestLoadFailsOnMissingDirectory(t *testing.T) {
	loader, err := filesystem.NewLoader(filepath.Join(t.TempDir(), "no-existe"))
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	if _, err := loader.Load(context.Background()); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}

func TestLoadHonorsCancellation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.md", "# A\n")

	loader, err := filesystem.NewLoader(dir)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := loader.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestNewLoaderRequiresDirectory(t *testing.T) {
	if _, err := filesystem.NewLoader("   "); err == nil {
		t.Error("expected an error for an empty directory")
	}
}
