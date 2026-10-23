// Package ports declares the interfaces the application needs from the outside
// world. Adapters implement them; app and domain never import an adapter.
package ports

import (
	"context"

	"github.com/r0lm0/doodle-assistant/internal/domain"
)

// Embedder turns text into vectors. It is always called in batches: one HTTP
// round trip per batch is far cheaper than one per text.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// VectorStore persists documents and their chunk embeddings.
//
// DocumentHash exists so the application can skip embedding work entirely when a
// file did not change: the hash is cheaper than N embedding calls.
type VectorStore interface {
	DocumentHash(ctx context.Context, sourcePath string) (hash string, found bool, err error)
	SaveDocument(ctx context.Context, doc domain.Document) (documentID int64, err error)
	Stats(ctx context.Context) (StoreStats, error)
}

// StoreStats is a cheap "what is in the index" summary used by the CLI.
type StoreStats struct {
	Documents int
	Chunks    int
}

// DocumentLoader lists the knowledge base: one Document per source file, with
// Content filled and Chunks empty (chunking belongs to the application).
type DocumentLoader interface {
	Load(ctx context.Context) ([]domain.Document, error)
}
