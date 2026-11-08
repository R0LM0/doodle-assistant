//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/r0lm0/doodle-assistant/internal/adapters/postgres"
	"github.com/r0lm0/doodle-assistant/internal/domain"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

// direction builds a one-hot vector: searching with it should rank that chunk first.
func direction(dim, index int) []float32 {
	v := make([]float32, dim)
	v[index] = 1
	return v
}

func TestSearchRanksByCosineSimilarity(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	sourcePath := fmt.Sprintf("integration/search-%d.md", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM documents WHERE source_path = $1`, sourcePath)
	})

	_, err = store.SaveDocument(ctx, domain.Document{
		SourcePath:  sourcePath,
		Title:       "Busqueda",
		ContentHash: "hash-search",
		Chunks: []domain.Chunk{
			{HeadingPath: "Busqueda > Escala", Position: 0, Content: "el tope es 1920x1080", Embedding: direction(768, 0)},
			{HeadingPath: "Busqueda > Fondo", Position: 1, Content: "el fondo puede ser transparente", Embedding: direction(768, 1)},
		},
	})
	if err != nil {
		t.Fatalf("SaveDocument: %v", err)
	}

	hits, err := store.Search(ctx, ports.SearchRequest{Query: "tope escala", Embedding: direction(768, 0), Limit: 2, Mode: ports.SearchModeVector})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].HeadingPath != "Busqueda > Escala" {
		t.Errorf("first hit = %q, want the closest chunk", hits[0].HeadingPath)
	}
	if hits[0].SourcePath != sourcePath || hits[0].Title != "Busqueda" {
		t.Errorf("provenance missing: %+v", hits[0])
	}
	if hits[0].Score < hits[1].Score {
		t.Errorf("hits are not sorted by score: %v then %v", hits[0].Score, hits[1].Score)
	}
	if hits[1].Content == "" {
		t.Error("chunk content not returned")
	}

	limited, err := store.Search(ctx, ports.SearchRequest{Query: "fondo", Embedding: direction(768, 1), Limit: 1, Mode: ports.SearchModeVector})
	if err != nil {
		t.Fatalf("Search with limit 1: %v", err)
	}
	if len(limited) != 1 || limited[0].HeadingPath != "Busqueda > Fondo" {
		t.Errorf("limit not honoured: %+v", limited)
	}

	if _, err := store.Search(ctx, ports.SearchRequest{Query: "x", Embedding: direction(768, 0), Limit: 0}); err == nil {
		t.Error("expected an error for limit 0")
	}
	if _, err := store.Search(ctx, ports.SearchRequest{Query: "x", Limit: 3}); err == nil {
		t.Error("expected an error for an empty embedding")
	}
}

func TestSearchLexicalFindsSpanishTerm(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	sourcePath := fmt.Sprintf("integration/lexical-%d.md", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM documents WHERE source_path = $1`, sourcePath)
	})

	_, err = store.SaveDocument(ctx, domain.Document{
		SourcePath:  sourcePath,
		Title:       "Busqueda",
		ContentHash: "hash-lexical",
		Chunks: []domain.Chunk{
			{HeadingPath: "Busqueda > Escala", Position: 0, Content: "el tope es 1920x1080 pixeles quimbamba", Embedding: direction(768, 0)},
			{HeadingPath: "Busqueda > Fondo", Position: 1, Content: "el fondo puede ser transparente en PNG quimbamba girasol", Embedding: direction(768, 0)},
		},
	})
	if err != nil {
		t.Fatalf("SaveDocument: %v", err)
	}

	// Both chunks share the same embedding, so only the lexical leg discriminates.
	// "girasol" appears only in Fondo, so the corpus cannot interfere.
	hits, err := store.Search(ctx, ports.SearchRequest{Query: "girasol", Embedding: direction(768, 0), Limit: 5, Mode: ports.SearchModeLexical})
	if err != nil {
		t.Fatalf("Lexical search: %v", err)
	}
	if len(hits) != 1 || hits[0].HeadingPath != "Busqueda > Fondo" {
		t.Fatalf("lexical hits = %+v, want only Fondo", hits)
	}
	if hits[0].SourcePath != sourcePath {
		t.Fatalf("lexical hit leaked from corpus: %+v", hits)
	}
	if hits[0].Score <= 0 {
		t.Errorf("lexical score = %v, want positive ts_rank", hits[0].Score)
	}

	if _, err := store.Search(ctx, ports.SearchRequest{Embedding: direction(768, 0), Limit: 2, Mode: ports.SearchModeLexical}); err == nil {
		t.Error("expected an error for lexical search without query")
	}
}

func TestSearchHybridFusesBothLegs(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	sourcePath := fmt.Sprintf("integration/hybrid-%d.md", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM documents WHERE source_path = $1`, sourcePath)
	})

	_, err = store.SaveDocument(ctx, domain.Document{
		SourcePath:  sourcePath,
		Title:       "Busqueda",
		ContentHash: "hash-hybrid",
		Chunks: []domain.Chunk{
			{HeadingPath: "Busqueda > Escala", Position: 0, Content: "el tope es 1920x1080 pixeles quimbamba", Embedding: direction(768, 0)},
			{HeadingPath: "Busqueda > Fondo", Position: 1, Content: "el fondo puede ser girasol en PNG quimbamba", Embedding: direction(768, 1)},
		},
	})
	if err != nil {
		t.Fatalf("SaveDocument: %v", err)
	}

	// Vector leg prefers Escala (one-hot 0), lexical leg prefers Fondo
	// ("girasol" is unique to this doc); hybrid must return both with RRF scores desc.
	hits, err := store.Search(ctx, ports.SearchRequest{Query: "girasol", Embedding: direction(768, 0), Limit: 5, Mode: ports.SearchModeHybrid})
	if err != nil {
		t.Fatalf("Hybrid search: %v", err)
	}
	if len(hits) < 2 {
		t.Fatalf("hybrid hits = %d, want at least 2", len(hits))
	}
	if hits[0].Score < hits[1].Score {
		t.Errorf("hybrid not sorted by score desc: %+v", hits)
	}
	seen := map[string]bool{}
	for _, h := range hits {
		if h.SourcePath == sourcePath {
			seen[h.HeadingPath] = true
		}
	}
	if !seen["Busqueda > Escala"] || !seen["Busqueda > Fondo"] {
		t.Errorf("hybrid must fuse both legs: %+v", hits)
	}
}
