package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/r0lm0/doodle-assistant/internal/ports"
)

// ToolSearchDocs lets the model decide when to search the documentation, instead
// of always searching as the classic pipeline does.
const ToolSearchDocs = "search_docs"

// NewSearchDocsTool wraps embed+search as a tool.
func NewSearchDocsTool(embedder ports.Embedder, store ports.VectorStore, topK int) (ports.Tool, error) {
	if embedder == nil {
		return nil, errors.New("search_docs: embedder is required")
	}
	if store == nil {
		return nil, errors.New("search_docs: vector store is required")
	}
	if topK <= 0 {
		topK = defaultTopK
	}
	return searchDocsTool{embedder: embedder, store: store, topK: topK}, nil
}

type searchDocsTool struct {
	embedder ports.Embedder
	store    ports.VectorStore
	topK     int
}

func (searchDocsTool) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        ToolSearchDocs,
		Description: "Busca en la documentación de la app y devuelve los fragmentos más relevantes con su archivo y encabezado. Usala cuando necesites un dato concreto (límites, atajos, formatos) en lugar de responder de memoria.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{"query":{"type":"string","description":"consulta en lenguaje natural, específica"}},
			"required":["query"],
			"additionalProperties":false
		}`),
	}
}

func (t searchDocsTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Query string `json:"query"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	if len(in.Query) > maxQuestionChars {
		return "", fmt.Errorf("search_docs: query is too long (%d chars, max %d)", len(in.Query), maxQuestionChars)
	}

	hits, err := searchHits(ctx, t.embedder, t.store, in.Query, t.topK)
	if err != nil {
		return "", fmt.Errorf("search_docs: %w", err)
	}

	type fragment struct {
		Source  string  `json:"source"`
		Heading string  `json:"heading"`
		Text    string  `json:"text"`
		Dist    float64 `json:"distance"`
	}
	out := struct {
		Hits []fragment `json:"hits"`
	}{Hits: make([]fragment, len(hits))}
	for i, hit := range hits {
		out.Hits[i] = fragment{Source: hit.SourcePath, Heading: hit.HeadingPath, Text: hit.Content, Dist: hit.Distance}
	}
	return toJSON(out)
}

// searchHits is the shared embed-one-query-then-search step.
func searchHits(ctx context.Context, embedder ports.Embedder, store ports.VectorStore, query string, topK int) ([]ports.SearchHit, error) {
	vectors, err := embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vectors) != 1 || len(vectors[0]) == 0 {
		return nil, fmt.Errorf("embedder returned %d vectors for one query", len(vectors))
	}
	hits, err := store.Search(ctx, vectors[0], topK)
	if err != nil {
		return nil, fmt.Errorf("search chunks: %w", err)
	}
	return hits, nil
}
