// Package app holds the use cases of the assistant: ingestion today, asking and
// running the agent next. It depends only on domain and ports.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/r0lm0/doodle-assistant/internal/domain"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

// defaultEmbedBatchSize keeps individual HTTP requests small: one call per 32
// chunks amortizes the round trip without holding a huge payload in memory.
const defaultEmbedBatchSize = 32

// IngestConfig is the tunable part of ingestion. Zero values fall back to safe
// defaults so a caller can pass only what it cares about.
type IngestConfig struct {
	ChunkOptions   domain.ChunkOptions
	EmbedBatchSize int
	Logger         *slog.Logger
}

// IngestDocs is the use case that turns the markdown knowledge base into chunks
// with embeddings, skipping files whose content did not change.
type IngestDocs struct {
	loader       ports.DocumentLoader
	embedder     ports.Embedder
	store        ports.VectorStore
	chunkOptions domain.ChunkOptions
	batchSize    int
	logger       *slog.Logger
}

// IngestStats reports what one ingestion run did, including where the time went.
type IngestStats struct {
	Scanned    int
	Skipped    int
	Indexed    int
	Chunks     int
	EmbedCalls int
	EmbedTime  time.Duration
	StoreTime  time.Duration
}

// NewIngestDocs wires the use case. Every dependency is required: a nil one is a
// programming error, not a runtime condition.
func NewIngestDocs(loader ports.DocumentLoader, embedder ports.Embedder, store ports.VectorStore, cfg IngestConfig) (*IngestDocs, error) {
	if loader == nil {
		return nil, errors.New("ingest docs: document loader is required")
	}
	if embedder == nil {
		return nil, errors.New("ingest docs: embedder is required")
	}
	if store == nil {
		return nil, errors.New("ingest docs: vector store is required")
	}

	if cfg.ChunkOptions.MaxChars <= 0 {
		cfg.ChunkOptions = domain.DefaultChunkOptions()
	}
	if cfg.EmbedBatchSize <= 0 {
		cfg.EmbedBatchSize = defaultEmbedBatchSize
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return &IngestDocs{
		loader:       loader,
		embedder:     embedder,
		store:        store,
		chunkOptions: cfg.ChunkOptions,
		batchSize:    cfg.EmbedBatchSize,
		logger:       cfg.Logger,
	}, nil
}

// Ingest walks the knowledge base and indexes what changed. Documents are
// independent: a failing document aborts the run and returns the stats collected
// so far, so the caller can see how far it got.
func (uc *IngestDocs) Ingest(ctx context.Context) (IngestStats, error) {
	var stats IngestStats

	if err := ctx.Err(); err != nil {
		return stats, err
	}

	docs, err := uc.loader.Load(ctx)
	if err != nil {
		return stats, fmt.Errorf("load documents: %w", err)
	}
	stats.Scanned = len(docs)

	for _, doc := range docs {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		if err := uc.ingestDocument(ctx, doc, &stats); err != nil {
			return stats, err
		}
	}

	return stats, nil
}

func (uc *IngestDocs) ingestDocument(ctx context.Context, doc domain.Document, stats *IngestStats) error {
	doc.ContentHash = domain.ContentHash(doc.Content)

	existingHash, found, err := uc.store.DocumentHash(ctx, doc.SourcePath)
	if err != nil {
		return fmt.Errorf("read content hash of %s: %w", doc.SourcePath, err)
	}
	// The whole point of the hash: unchanged content costs one indexed SELECT
	// instead of a chunker pass plus N embedding calls.
	if found && existingHash == doc.ContentHash {
		stats.Skipped++
		uc.logger.Info("skipped unchanged document", "path", doc.SourcePath)
		return nil
	}

	chunks := domain.ChunkMarkdown(doc.Content, uc.chunkOptions)
	if len(chunks) == 0 {
		stats.Skipped++
		uc.logger.Warn("document produced no chunks", "path", doc.SourcePath)
		return nil
	}

	embedStart := time.Now()
	calls, err := uc.embedChunks(ctx, chunks)
	if err != nil {
		return fmt.Errorf("embed %s: %w", doc.SourcePath, err)
	}
	embedTook := time.Since(embedStart)

	doc.Chunks = chunks

	storeStart := time.Now()
	if _, err := uc.store.SaveDocument(ctx, doc); err != nil {
		return fmt.Errorf("save %s: %w", doc.SourcePath, err)
	}
	storeTook := time.Since(storeStart)

	stats.Indexed++
	stats.Chunks += len(chunks)
	stats.EmbedCalls += calls
	stats.EmbedTime += embedTook
	stats.StoreTime += storeTook

	uc.logger.Info("indexed document",
		"path", doc.SourcePath,
		"chunks", len(chunks),
		"embed_ms", embedTook.Milliseconds(),
		"store_ms", storeTook.Milliseconds(),
	)
	return nil
}

// embedChunks fills Chunk.Embedding in place, batch by batch. It validates what
// comes back: a model that returns fewer vectors or shorter ones is a broken
// contract, and silently accepting it would poison the index.
func (uc *IngestDocs) embedChunks(ctx context.Context, chunks []domain.Chunk) (int, error) {
	var dim int
	calls := 0

	for start := 0; start < len(chunks); start += uc.batchSize {
		end := start + uc.batchSize
		if end > len(chunks) {
			end = len(chunks)
		}

		texts := make([]string, 0, end-start)
		for _, chunk := range chunks[start:end] {
			texts = append(texts, chunk.EmbeddingText())
		}

		vectors, err := uc.embedder.Embed(ctx, texts)
		if err != nil {
			return calls, fmt.Errorf("embed batch [%d:%d]: %w", start, end, err)
		}
		calls++
		if len(vectors) != len(texts) {
			return calls, fmt.Errorf("embed batch [%d:%d]: got %d vectors for %d texts", start, end, len(vectors), len(texts))
		}

		for i, vec := range vectors {
			if len(vec) == 0 {
				return calls, fmt.Errorf("embed batch [%d:%d]: empty vector at offset %d", start, end, i)
			}
			if dim == 0 {
				dim = len(vec)
			} else if len(vec) != dim {
				return calls, fmt.Errorf("embed batch [%d:%d]: vector %d has dimension %d, expected %d", start, end, i, len(vec), dim)
			}
			chunks[start+i].Embedding = vec
		}
	}

	return calls, nil
}
