package memory_test

import (
	"context"
	"testing"

	"github.com/r0lm0/doodle-assistant/internal/adapters/memory"
	"github.com/r0lm0/doodle-assistant/internal/domain"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

func seedStore(t *testing.T) *memory.VectorStore {
	t.Helper()
	s := memory.NewVectorStore()
	_, err := s.SaveDocument(context.Background(), domain.Document{
		SourcePath:  "export.md",
		Title:       "Exportar",
		ContentHash: "h1",
		Chunks: []domain.Chunk{
			{HeadingPath: "Exportar > Escala", Position: 0, Content: "la escala va de 1x a 2x tope 1920", Embedding: []float32{1, 0}},
			{HeadingPath: "Exportar > Fondo", Position: 1, Content: "el fondo transparente solo en PNG", Embedding: []float32{0, 1}},
		},
	})
	if err != nil {
		t.Fatalf("SaveDocument: %v", err)
	}
	return s
}

func TestSearchVectorRanksBySimilarity(t *testing.T) {
	s := seedStore(t)
	hits, err := s.Search(context.Background(), ports.SearchRequest{
		Query: "escala", Embedding: []float32{1, 0}, Limit: 2, Mode: ports.SearchModeVector,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 || hits[0].HeadingPath != "Exportar > Escala" {
		t.Fatalf("vector order = %+v", hits)
	}
	if hits[0].Score < hits[1].Score {
		t.Errorf("vector not sorted by score desc: %v then %v", hits[0].Score, hits[1].Score)
	}
}

func TestSearchEmptyModeMeansVector(t *testing.T) {
	s := seedStore(t)
	hits, err := s.Search(context.Background(), ports.SearchRequest{
		Query: "escala", Embedding: []float32{1, 0}, Limit: 1,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].HeadingPath != "Exportar > Escala" {
		t.Fatalf("empty mode = %+v", hits)
	}
}

func TestSearchLexicalFindsExactTerm(t *testing.T) {
	s := seedStore(t)
	hits, err := s.Search(context.Background(), ports.SearchRequest{
		Query: "transparente PNG", Embedding: []float32{1, 0}, Limit: 2, Mode: ports.SearchModeLexical,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 || hits[0].HeadingPath != "Exportar > Fondo" {
		t.Fatalf("lexical order = %+v", hits)
	}
}

func TestSearchHybridReturnsRRFScore(t *testing.T) {
	s := seedStore(t)
	hits, err := s.Search(context.Background(), ports.SearchRequest{
		Query: "transparente", Embedding: []float32{1, 0}, Limit: 2, Mode: ports.SearchModeHybrid,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d", len(hits))
	}
	if hits[0].Score <= 0 || hits[0].Score < hits[1].Score {
		t.Errorf("hybrid scores not RRF-sorted: %+v", hits)
	}
}

func TestSearchRejectsUnknownMode(t *testing.T) {
	s := seedStore(t)
	if _, err := s.Search(context.Background(), ports.SearchRequest{
		Query: "x", Embedding: []float32{1, 0}, Limit: 1, Mode: "bm25",
	}); err == nil {
		t.Error("expected error for unknown mode")
	}
}
