// Package memory provides in-memory fakes for tests and, later, the in-memory
// canvas repository of the tool-calling phase.
package memory

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"

	"github.com/r0lm0/doodle-assistant/internal/domain"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

var (
	_ ports.Embedder    = (*Embedder)(nil)
	_ ports.VectorStore = (*VectorStore)(nil)
)

// Embedder is a deterministic fake: the same text always yields the same vector,
// so tests can assert on identity instead of on model behaviour.
type Embedder struct {
	Dim int

	mu    sync.Mutex
	Calls [][]string
	Err   error
}

// NewEmbedder returns a fake embedder with a small dimension; the real dimension
// is irrelevant for application tests.
func NewEmbedder(dim int) *Embedder {
	return &Embedder{Dim: dim}
}

func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	e.mu.Lock()
	e.Calls = append(e.Calls, append([]string(nil), texts...))
	err := e.Err
	e.mu.Unlock()

	if err != nil {
		return nil, err
	}

	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = e.vector(text)
	}
	return out, nil
}

func (e *Embedder) vector(text string) []float32 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	seed := h.Sum64()

	vec := make([]float32, e.Dim)
	for i := range vec {
		seed = seed*6364136223846793005 + 1442695040888963407
		vec[i] = float32((seed>>33)%1000) / 1000
	}
	return vec
}

// CallCount reports how many Embed calls happened, i.e. how many batches were
// sent. Tests assert on this to prove batching.
func (e *Embedder) CallCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.Calls)
}

// BatchSizes returns the size of every batch, in call order.
func (e *Embedder) BatchSizes() []int {
	e.mu.Lock()
	defer e.mu.Unlock()

	sizes := make([]int, len(e.Calls))
	for i, call := range e.Calls {
		sizes[i] = len(call)
	}
	return sizes
}

// VectorStore is an in-memory VectorStore that replaces a document's chunks on
// every save, exactly like the Postgres adapter does.
type VectorStore struct {
	mu     sync.Mutex
	docs   map[string]domain.Document
	saves  int
	nextID int64
	Err    error
}

func NewVectorStore() *VectorStore {
	return &VectorStore{docs: make(map[string]domain.Document), nextID: 1}
}

func (s *VectorStore) DocumentHash(ctx context.Context, sourcePath string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	doc, ok := s.docs[sourcePath]
	if !ok {
		return "", false, nil
	}
	return doc.ContentHash, true, nil
}

func (s *VectorStore) SaveDocument(ctx context.Context, doc domain.Document) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Err != nil {
		return 0, s.Err
	}

	if doc.SourcePath == "" {
		return 0, fmt.Errorf("save document: empty source path")
	}
	if doc.ID == 0 {
		doc.ID = s.nextID
		s.nextID++
	}

	stored := domain.Document{
		ID:          doc.ID,
		SourcePath:  doc.SourcePath,
		Title:       doc.Title,
		ContentHash: doc.ContentHash,
		Content:     doc.Content,
		Chunks:      make([]domain.Chunk, len(doc.Chunks)),
	}
	for i, chunk := range doc.Chunks {
		chunk.ID = int64(i + 1)
		chunk.DocumentID = doc.ID
		stored.Chunks[i] = chunk
	}

	s.docs[doc.SourcePath] = stored
	s.saves++
	return doc.ID, nil
}

func (s *VectorStore) Stats(ctx context.Context) (ports.StoreStats, error) {
	if err := ctx.Err(); err != nil {
		return ports.StoreStats{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stats := ports.StoreStats{Documents: len(s.docs)}
	for _, doc := range s.docs {
		stats.Chunks += len(doc.Chunks)
	}
	return stats, nil
}

// SaveCount reports how many times SaveDocument ran.
func (s *VectorStore) SaveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saves
}

// Document returns a stored document by source path.
func (s *VectorStore) Document(sourcePath string) (domain.Document, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	doc, ok := s.docs[sourcePath]
	return doc, ok
}

// ChunkContents returns the chunk text of a stored document, in position order.
func (s *VectorStore) ChunkContents(sourcePath string) []string {
	doc, ok := s.Document(sourcePath)
	if !ok {
		return nil
	}

	contents := make([]string, len(doc.Chunks))
	for i, chunk := range doc.Chunks {
		contents[i] = chunk.Content
	}
	return contents
}
