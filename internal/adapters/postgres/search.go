package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/pgvector/pgvector-go"

	"github.com/r0lm0/doodle-assistant/internal/ports"
)

// Search returns ranked chunks according to req.Mode (empty means vector).
// Vector orders by cosine similarity desc, lexical by ts_rank desc (spanish),
// hybrid fuses both legs with RRF. The join supplies the provenance
// that the answer has to cite, so callers never guess which file a chunk is from.
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

	switch mode {
	case ports.SearchModeVector:
		return s.searchVector(ctx, req)
	case ports.SearchModeLexical:
		return s.searchLexical(ctx, req)
	case ports.SearchModeHybrid:
		k := s.RRFK
		if k <= 0 {
			k = rrfKDefault
		}
		return s.searchHybrid(ctx, req, k)
	default:
		return nil, fmt.Errorf("postgres store: unknown search mode %q", string(mode))
	}
}

// rrfKDefault is the RRF rank offset. T3 exposes it as RRF_K; until then the
// standard 60 keeps vector and lexical comparable with the memory fake.
const rrfKDefault = 60

func (s *Store) searchVector(ctx context.Context, req ports.SearchRequest) ([]ports.SearchHit, error) {
	const q = `
		SELECT d.source_path, d.title, c.heading_path, c.position, c.content,
		       1 - (c.embedding <=> $1) AS score
		FROM chunks c
		JOIN documents d ON d.id = c.document_id
		ORDER BY score DESC
		LIMIT $2`

	return s.queryHits(ctx, q, pgvector.NewVector(req.Embedding), req.Limit)
}

func (s *Store) searchLexical(ctx context.Context, req ports.SearchRequest) ([]ports.SearchHit, error) {
	if req.Query == "" {
		return nil, errors.New("postgres store: lexical search needs a query")
	}
	const q = `
		SELECT d.source_path, d.title, c.heading_path, c.position, c.content,
		       ts_rank(c.content_tsv, websearch_to_tsquery('spanish', $1)) AS score
		FROM chunks c
		JOIN documents d ON d.id = c.document_id
		WHERE c.content_tsv @@ websearch_to_tsquery('spanish', $1)
		ORDER BY score DESC
		LIMIT $2`

	return s.queryHits(ctx, q, req.Query, req.Limit)
}

func (s *Store) searchHybrid(ctx context.Context, req ports.SearchRequest, rrfK float64) ([]ports.SearchHit, error) {
	if req.Query == "" {
		return nil, errors.New("postgres store: hybrid search needs a query")
	}
	const q = `
		WITH vec AS (
			SELECT c.id, row_number() OVER (ORDER BY c.embedding <=> $1) AS rank
			FROM chunks c
			ORDER BY c.embedding <=> $1
			LIMIT $3
		), lex AS (
			SELECT c.id, row_number() OVER (ORDER BY ts_rank(c.content_tsv, q) DESC) AS rank
			FROM chunks c, websearch_to_tsquery('spanish', $2) q
			WHERE c.content_tsv @@ q
			ORDER BY 2
			LIMIT $3
		)
		SELECT d.source_path, d.title, c.heading_path, c.position, c.content,
		       COALESCE(1.0/($4+vec.rank),0) + COALESCE(1.0/($4+lex.rank),0) AS score
		FROM chunks c
		JOIN documents d ON d.id = c.document_id
		LEFT JOIN vec ON vec.id = c.id
		LEFT JOIN lex ON lex.id = c.id
		WHERE vec.id IS NOT NULL OR lex.id IS NOT NULL
		ORDER BY score DESC
		LIMIT $5`

	return s.queryHits(ctx, q,
		pgvector.NewVector(req.Embedding), req.Query, req.Limit, rrfK, req.Limit)
}

func (s *Store) queryHits(ctx context.Context, q string, args ...any) ([]ports.SearchHit, error) {
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("search chunks: %w", err)
	}
	defer rows.Close()

	hits := make([]ports.SearchHit, 0)
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
