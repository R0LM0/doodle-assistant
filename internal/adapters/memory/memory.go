// Package memory provides in-memory fakes for tests and, later, the in-memory
// canvas repository of the tool-calling phase.
package memory

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/r0lm0/doodle-assistant/internal/domain"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

var (
	_ ports.Embedder    = (*Embedder)(nil)
	_ ports.VectorStore = (*VectorStore)(nil)
	_ ports.ChatModel   = (*ChatModel)(nil)
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

// Search ranks every stored chunk by cosine distance, brute force. The fake is for
// tests, not for scale: it exists to prove the use case wires retrieval correctly.
func (s *VectorStore) Search(ctx context.Context, embedding []float32, limit int) ([]ports.SearchHit, error) {
	if len(embedding) == 0 {
		return nil, errors.New("memory store: search embedding is empty")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("memory store: search limit must be positive, got %d", limit)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	hits := make([]ports.SearchHit, 0, len(s.docs))
	for _, doc := range s.docs {
		for _, chunk := range doc.Chunks {
			hits = append(hits, ports.SearchHit{
				SourcePath:  doc.SourcePath,
				Title:       doc.Title,
				HeadingPath: chunk.HeadingPath,
				Position:    chunk.Position,
				Content:     chunk.Content,
				Distance:    cosineDistance(embedding, chunk.Embedding),
			})
		}
	}

	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Distance < hits[j].Distance })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func cosineDistance(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 1
	}

	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 1
	}
	return 1 - dot/(math.Sqrt(normA)*math.Sqrt(normB))
}

// ChatTurn is one scripted model answer: text deltas, requested tool calls, or an
// error. Scripting turns is what makes an agent loop testable without a model.
type ChatTurn struct {
	Deltas    []string
	ToolCalls []ports.ToolCall
	Err       error
}

// ChatModel is a fake chat model. With Turns it plays a script, one turn per call;
// with only Deltas it answers the same way every time.
type ChatModel struct {
	mu            sync.Mutex
	Conversations [][]ports.Message
	Requests      []ports.ChatRequest
	Turns         []ChatTurn
	Deltas        []string
	Err           error

	next int
}

func (c *ChatModel) Chat(ctx context.Context, req ports.ChatRequest, emit func(delta string) error) (ports.ChatResponse, error) {
	var response ports.ChatResponse

	if err := ctx.Err(); err != nil {
		return response, err
	}

	c.mu.Lock()
	c.Conversations = append(c.Conversations, append([]ports.Message(nil), req.Messages...))
	c.Requests = append(c.Requests, req)

	turn := ChatTurn{Deltas: append([]string(nil), c.Deltas...), Err: c.Err}
	if len(c.Turns) > 0 {
		index := c.next
		if index >= len(c.Turns) {
			index = len(c.Turns) - 1
		}
		turn = c.Turns[index]
		c.next++
	}
	c.mu.Unlock()

	if turn.Err != nil {
		return response, turn.Err
	}

	var text strings.Builder
	for _, delta := range turn.Deltas {
		text.WriteString(delta)
		if err := emit(delta); err != nil {
			return response, err
		}
	}

	response.Content = text.String()
	response.ToolCalls = turn.ToolCalls
	return response, nil
}

// LastConversation returns the messages sent in the most recent call.
func (c *ChatModel) LastConversation() []ports.Message {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.Conversations) == 0 {
		return nil
	}
	return c.Conversations[len(c.Conversations)-1]
}

// LastRequest returns the full request of the most recent call, including tools.
func (c *ChatModel) LastRequest() (ports.ChatRequest, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.Requests) == 0 {
		return ports.ChatRequest{}, false
	}
	return c.Requests[len(c.Requests)-1], true
}

// CallCount reports how many model calls happened.
func (c *ChatModel) CallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.Requests)
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
