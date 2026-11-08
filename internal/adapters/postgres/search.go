package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/pgvector/pgvector-go"

	"github.com/r0lm0/doodle-assistant/internal/ports"
)

// Search returns the closest chunks to req.Embedding, ordered by cosine similarity
// (descending: similarity 1 is a perfect match). The join supplies the provenance
// that the answer has to cite, so callers never guess which file a chunk is from.
// Only SearchModeVector (and empty, which means vector) is supported here;
// lexical and hybrid arrive in T2 with migration 0003.
func (s *Store) Search(ctx context.Context, req ports.SearchRequest) ([]ports.SearchHit, error) {
	if len(req.Embedding) == 0 {
		return nil, errors.New("postgres store: search embedding is empty")
	}
	if req.Limit <= 0 {
		return nil, fmt.Errorf("postgres store: search limit must be positive, got %d", req.Limit)
	}
	mode := req.Mode
	if mode == "" {
		mode = ports.SearchModeVector
	}
	if mode != ports.SearchModeVector {
		return nil, fmt.Errorf("postgres store: search mode %q not implemented yet", string(mode))
	}

	const q = `
		SELECT d.source_path, d.title, c.heading_path, c.position, c.content,
		       1 - (c.embedding <=> $1) AS score
		FROM chunks c
		JOIN documents d ON d.id = c.document_id
		ORDER BY score DESC
		LIMIT $2`

	rows, err := s.pool.Query(ctx, q, pgvector.NewVector(req.Embedding), req.Limit)
	if err != nil {
		return nil, fmt.Errorf("search chunks: %w", err)
	}
	defer rows.Close()

	hits := make([]ports.SearchHit, 0, req.Limit)
	for rows.Next() {
		var hit ports.SearchHit
		if err := rows.Scan(&hit.SourcePath, &hit.Title, &hit.HeadingPath, &hit.Position, &hit.Content, &hit.Score); err != nil {
			return nil, fmt.Errorf("scan search hit: %w", err)
		}
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search hits: %w", err)
	}

	return hits, nil
}
