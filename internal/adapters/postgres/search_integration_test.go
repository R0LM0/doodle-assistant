//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/r0lm0/doodle-assistant/internal/adapters/postgres"
	"github.com/r0lm0/doodle-assistant/internal/domain"
)

// direction builds a one-hot vector: searching with it should rank that chunk first.
func direction(dim, index int) []float32 {
	v := make([]float32, dim)
	v[index] = 1
	return v
}

func TestSearchRanksByCosineDistance(t *testing.T) {
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

	hits, err := store.Search(ctx, direction(768, 0), 2)
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
	if hits[0].Distance > hits[1].Distance {
		t.Errorf("hits are not sorted by distance: %v then %v", hits[0].Distance, hits[1].Distance)
	}
	if hits[1].Content == "" {
		t.Error("chunk content not returned")
	}

	limited, err := store.Search(ctx, direction(768, 1), 1)
	if err != nil {
		t.Fatalf("Search with limit 1: %v", err)
	}
	if len(limited) != 1 || limited[0].HeadingPath != "Busqueda > Fondo" {
		t.Errorf("limit not honoured: %+v", limited)
	}

	if _, err := store.Search(ctx, direction(768, 0), 0); err == nil {
		t.Error("expected an error for limit 0")
	}
	if _, err := store.Search(ctx, nil, 3); err == nil {
		t.Error("expected an error for an empty embedding")
	}
}
