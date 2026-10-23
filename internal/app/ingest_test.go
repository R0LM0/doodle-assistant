package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/r0lm0/doodle-assistant/internal/adapters/memory"
	"github.com/r0lm0/doodle-assistant/internal/app"
	"github.com/r0lm0/doodle-assistant/internal/domain"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

const exportMD = `# Exportar

## Escala

La escala va de 1x a 2x, con tope de 1920x1080.

## Fondo

Puede ser solido o transparente.
`

func testConfig(batch int) app.IngestConfig {
	return app.IngestConfig{
		ChunkOptions:   domain.ChunkOptions{MaxChars: 1000, OverlapChars: 0, MinChars: 0},
		EmbedBatchSize: batch,
	}
}

func mustIngest(t *testing.T, loader ports.DocumentLoader, emb ports.Embedder, store ports.VectorStore, cfg app.IngestConfig) *app.IngestDocs {
	t.Helper()

	uc, err := app.NewIngestDocs(loader, emb, store, cfg)
	if err != nil {
		t.Fatalf("NewIngestDocs: %v", err)
	}
	return uc
}

func TestIngestIndexesNewDocument(t *testing.T) {
	loader := &fakeLoader{docs: []domain.Document{{SourcePath: "docs/export.md", Title: "Exportar", Content: exportMD}}}
	emb := memory.NewEmbedder(8)
	store := memory.NewVectorStore()
	uc := mustIngest(t, loader, emb, store, testConfig(32))

	stats, err := uc.Ingest(context.Background())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if stats.Scanned != 1 || stats.Indexed != 1 || stats.Skipped != 0 || stats.Chunks != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if store.SaveCount() != 1 {
		t.Errorf("saves = %d, want 1", store.SaveCount())
	}
	contents := store.ChunkContents("docs/export.md")
	if len(contents) != 2 {
		t.Fatalf("stored chunks = %d, want 2", len(contents))
	}
	doc, ok := store.Document("docs/export.md")
	if !ok {
		t.Fatal("document not stored")
	}
	if want := domain.IngestHash(exportMD, testConfig(32).ChunkOptions); doc.ContentHash != want {
		t.Errorf("stored hash = %q, want the content+chunking hash %q", doc.ContentHash, want)
	}
	for i, chunk := range doc.Chunks {
		if len(chunk.Embedding) != 8 {
			t.Errorf("chunk %d embedding dim = %d, want 8", i, len(chunk.Embedding))
		}
		if chunk.HeadingPath == "" {
			t.Errorf("chunk %d lost its heading path", i)
		}
	}
}

func TestIngestIsIdempotentWhenContentIsUnchanged(t *testing.T) {
	loader := &fakeLoader{docs: []domain.Document{{SourcePath: "docs/export.md", Title: "Exportar", Content: exportMD}}}
	emb := memory.NewEmbedder(8)
	store := memory.NewVectorStore()
	uc := mustIngest(t, loader, emb, store, testConfig(32))

	if _, err := uc.Ingest(context.Background()); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	firstCalls := emb.CallCount()

	stats, err := uc.Ingest(context.Background())
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}

	if stats.Skipped != 1 || stats.Indexed != 0 || stats.Chunks != 0 {
		t.Fatalf("second run stats = %+v, want 1 skipped and nothing indexed", stats)
	}
	if emb.CallCount() != firstCalls {
		t.Errorf("re-indexing unchanged content called the embedder again: %d -> %d calls", firstCalls, emb.CallCount())
	}
	if store.SaveCount() != 1 {
		t.Errorf("unchanged content was saved again: %d saves", store.SaveCount())
	}
}

func TestIngestReindexesWhenContentChanges(t *testing.T) {
	loader := &fakeLoader{docs: []domain.Document{{SourcePath: "docs/export.md", Title: "Exportar", Content: exportMD}}}
	emb := memory.NewEmbedder(8)
	store := memory.NewVectorStore()
	uc := mustIngest(t, loader, emb, store, testConfig(32))

	if _, err := uc.Ingest(context.Background()); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}

	loader.docs[0].Content = "# Exportar\n\n## Escala\n\nAhora el tope es 4K.\n"

	stats, err := uc.Ingest(context.Background())
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}

	if stats.Indexed != 1 || stats.Skipped != 0 || stats.Chunks != 1 {
		t.Fatalf("second run stats = %+v", stats)
	}
	contents := store.ChunkContents("docs/export.md")
	if len(contents) != 1 {
		t.Fatalf("old chunks were not replaced: %d chunks", len(contents))
	}
	if !strings.Contains(contents[0], "4K") {
		t.Errorf("stored chunk is stale: %q", contents[0])
	}
	if emb.CallCount() != 2 {
		t.Errorf("embed calls = %d, want 2 (one per version)", emb.CallCount())
	}
}

func TestIngestWrapsEmbedderErrorsAndStoresNothing(t *testing.T) {
	loader := &fakeLoader{docs: []domain.Document{{SourcePath: "docs/export.md", Title: "Exportar", Content: exportMD}}}
	cause := errors.New("ollama is down")
	emb := memory.NewEmbedder(8)
	emb.Err = cause
	store := memory.NewVectorStore()
	uc := mustIngest(t, loader, emb, store, testConfig(32))

	_, err := uc.Ingest(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, cause) {
		t.Errorf("error does not wrap the cause: %v", err)
	}
	if store.SaveCount() != 0 {
		t.Errorf("a failed embedding still saved %d documents", store.SaveCount())
	}
}

func TestIngestBatchesEmbeddingCalls(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Doc\n\n")
	for i := 0; i < 5; i++ {
		b.WriteString("## Seccion ")
		b.WriteString(string(rune('A' + i)))
		b.WriteString("\n\ntexto ")
		b.WriteString(string(rune('a' + i)))
		b.WriteString("\n\n")
	}

	loader := &fakeLoader{docs: []domain.Document{{SourcePath: "docs/doc.md", Title: "Doc", Content: b.String()}}}
	emb := memory.NewEmbedder(4)
	store := memory.NewVectorStore()
	uc := mustIngest(t, loader, emb, store, testConfig(2))

	stats, err := uc.Ingest(context.Background())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if stats.Chunks != 5 {
		t.Fatalf("chunks = %d, want 5", stats.Chunks)
	}
	if stats.EmbedCalls != 3 {
		t.Fatalf("stats.EmbedCalls = %d, want 3 (the number of batches actually sent)", stats.EmbedCalls)
	}
	want := []int{2, 2, 1}
	got := emb.BatchSizes()
	if len(got) != len(want) {
		t.Fatalf("batch sizes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("batch sizes = %v, want %v", got, want)
		}
	}
}

func TestIngestSkipsDocumentsWithoutChunks(t *testing.T) {
	loader := &fakeLoader{docs: []domain.Document{{SourcePath: "docs/empty.md", Title: "Vacio", Content: "# Solo\n\n## Encabezados\n"}}}
	emb := memory.NewEmbedder(4)
	store := memory.NewVectorStore()
	uc := mustIngest(t, loader, emb, store, testConfig(32))

	stats, err := uc.Ingest(context.Background())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if stats.Skipped != 1 || stats.Indexed != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if emb.CallCount() != 0 {
		t.Errorf("embedder was called %d times for an empty document", emb.CallCount())
	}
	if store.SaveCount() != 0 {
		t.Errorf("empty document was saved")
	}
}

func TestIngestHonorsContextCancellation(t *testing.T) {
	loader := &fakeLoader{docs: []domain.Document{{SourcePath: "docs/export.md", Title: "Exportar", Content: exportMD}}}
	emb := memory.NewEmbedder(4)
	store := memory.NewVectorStore()
	uc := mustIngest(t, loader, emb, store, testConfig(32))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := uc.Ingest(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if store.SaveCount() != 0 {
		t.Errorf("cancelled ingest saved data")
	}
}

func TestNewIngestDocsRejectsMissingDependencies(t *testing.T) {
	store := memory.NewVectorStore()
	emb := memory.NewEmbedder(4)

	if _, err := app.NewIngestDocs(nil, emb, store, testConfig(32)); err == nil {
		t.Error("expected an error for a nil loader")
	}
	if _, err := app.NewIngestDocs(&fakeLoader{}, nil, store, testConfig(32)); err == nil {
		t.Error("expected an error for a nil embedder")
	}
	if _, err := app.NewIngestDocs(&fakeLoader{}, emb, nil, testConfig(32)); err == nil {
		t.Error("expected an error for a nil store")
	}
}

type fakeLoader struct {
	docs []domain.Document
	err  error
}

func (f *fakeLoader) Load(ctx context.Context) ([]domain.Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.docs, nil
}

func TestIngestReindexesWhenChunkOptionsChange(t *testing.T) {
	loader := &fakeLoader{docs: []domain.Document{{SourcePath: "docs/export.md", Title: "Exportar", Content: exportMD}}}
	emb := memory.NewEmbedder(8)
	store := memory.NewVectorStore()

	first := mustIngest(t, loader, emb, store, testConfig(32))
	firstStats, err := first.Ingest(context.Background())
	if err != nil {
		t.Fatalf("first Ingest: %v", err)
	}

	// Same content, different chunk size: the index must be rebuilt, not skipped.
	finer := mustIngest(t, loader, emb, store, app.IngestConfig{
		ChunkOptions:   domain.ChunkOptions{MaxChars: 40, OverlapChars: 0, MinChars: 0},
		EmbedBatchSize: 32,
	})
	secondStats, err := finer.Ingest(context.Background())
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}

	if secondStats.Indexed != 1 || secondStats.Skipped != 0 {
		t.Fatalf("stats = %+v, want the document re-indexed", secondStats)
	}
	if secondStats.Chunks <= firstStats.Chunks {
		t.Errorf("chunks did not change with a smaller max size: %d -> %d", firstStats.Chunks, secondStats.Chunks)
	}
	if emb.CallCount() < 2 {
		t.Errorf("embed calls = %d, want a second embedding pass", emb.CallCount())
	}
}
