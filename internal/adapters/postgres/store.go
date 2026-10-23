// Package postgres implements the VectorStore port on top of pgx and pgvector.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"

	"github.com/r0lm0/doodle-assistant/internal/domain"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

var _ ports.VectorStore = (*Store)(nil)

// Store is the pgvector-backed VectorStore.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, errors.New("postgres store: pool is required")
	}
	return &Store{pool: pool}, nil
}

// DocumentHash returns the stored content hash of a source path.
func (s *Store) DocumentHash(ctx context.Context, sourcePath string) (string, bool, error) {
	const q = `SELECT content_hash FROM documents WHERE source_path = $1`

	var hash string
	err := s.pool.QueryRow(ctx, q, sourcePath).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query content hash of %s: %w", sourcePath, err)
	}
	return hash, true, nil
}

// SaveDocument upserts the document and replaces its chunks in one transaction.
// Readers never see a document with half of its new chunks, and a failure leaves
// the previous version untouched.
func (s *Store) SaveDocument(ctx context.Context, doc domain.Document) (int64, error) {
	if doc.SourcePath == "" {
		return 0, errors.New("postgres store: document source path is required")
	}
	if doc.ContentHash == "" {
		return 0, errors.New("postgres store: document content hash is required")
	}
	if len(doc.Chunks) == 0 {
		return 0, errors.New("postgres store: document has no chunks")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	// Rollback is a no-op after a successful Commit.
	defer func() { _ = tx.Rollback(ctx) }()

	const upsert = `
		INSERT INTO documents (source_path, title, content_hash)
		VALUES ($1, $2, $3)
		ON CONFLICT (source_path) DO UPDATE
		SET title = EXCLUDED.title,
		    content_hash = EXCLUDED.content_hash,
		    updated_at = now()
		RETURNING id`

	var documentID int64
	if err := tx.QueryRow(ctx, upsert, doc.SourcePath, doc.Title, doc.ContentHash).Scan(&documentID); err != nil {
		return 0, fmt.Errorf("upsert document %s: %w", doc.SourcePath, err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM chunks WHERE document_id = $1`, documentID); err != nil {
		return 0, fmt.Errorf("delete old chunks of %s: %w", doc.SourcePath, err)
	}

	const insertChunk = `
		INSERT INTO chunks (document_id, heading_path, position, content, embedding)
		VALUES ($1, $2, $3, $4, $5)`

	// One batch instead of one round trip per chunk: same transaction, ~1 network
	// wait. The dimension is not validated here on purpose: vector(768) is the
	// schema's job, and a wrong dimension must fail loudly at the database.
	batch := &pgx.Batch{}
	for _, chunk := range doc.Chunks {
		batch.Queue(insertChunk, documentID, chunk.HeadingPath, chunk.Position, chunk.Content, pgvector.NewVector(chunk.Embedding))
	}

	results := tx.SendBatch(ctx, batch)
	for i := range doc.Chunks {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return 0, fmt.Errorf("insert chunk %d of %s: %w", i, doc.SourcePath, err)
		}
	}
	if err := results.Close(); err != nil {
		return 0, fmt.Errorf("close chunk batch of %s: %w", doc.SourcePath, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit %s: %w", doc.SourcePath, err)
	}
	return documentID, nil
}

// Stats returns how much is indexed.
func (s *Store) Stats(ctx context.Context) (ports.StoreStats, error) {
	const q = `SELECT (SELECT count(*) FROM documents), (SELECT count(*) FROM chunks)`

	var stats ports.StoreStats
	if err := s.pool.QueryRow(ctx, q).Scan(&stats.Documents, &stats.Chunks); err != nil {
		return ports.StoreStats{}, fmt.Errorf("query stats: %w", err)
	}
	return stats, nil
}
