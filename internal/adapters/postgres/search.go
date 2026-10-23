package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/pgvector/pgvector-go"

	"github.com/r0lm0/doodle-assistant/internal/ports"
)

// Search returns the closest chunks to embedding, ordered by cosine distance
// (ascending: distance 0 is a perfect match). The join supplies the provenance
// that the answer has to cite, so callers never guess which file a chunk is from.
func (s *Store) Search(ctx context.Context, embedding []float32, limit int) ([]ports.SearchHit, error) {
	if len(embedding) == 0 {
		return nil, errors.New("postgres store: search embedding is empty")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("postgres store: search limit must be positive, got %d", limit)
	}

	const q = `
		SELECT d.source_path, d.title, c.heading_path, c.position, c.content,
		       c.embedding <=> $1 AS distance
		FROM chunks c
		JOIN documents d ON d.id = c.document_id
		ORDER BY distance ASC
		LIMIT $2`

	rows, err := s.pool.Query(ctx, q, pgvector.NewVector(embedding), limit)
	if err != nil {
		return nil, fmt.Errorf("search chunks: %w", err)
	}
	defer rows.Close()

	hits := make([]ports.SearchHit, 0, limit)
	for rows.Next() {
		var hit ports.SearchHit
		if err := rows.Scan(&hit.SourcePath, &hit.Title, &hit.HeadingPath, &hit.Position, &hit.Content, &hit.Distance); err != nil {
			return nil, fmt.Errorf("scan search hit: %w", err)
		}
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search hits: %w", err)
	}

	return hits, nil
}
