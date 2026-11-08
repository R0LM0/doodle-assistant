// Package ports declares the interfaces the application needs from the outside
// world. Adapters implement them; app and domain never import an adapter.
package ports

import (
	"context"
	"encoding/json"

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
	Search(ctx context.Context, req SearchRequest) ([]SearchHit, error)
	Stats(ctx context.Context) (StoreStats, error)
}

// SearchMode selects which index leg answers the query.
type SearchMode string

const (
	SearchModeVector  SearchMode = "vector"
	SearchModeLexical SearchMode = "lexical"
	SearchModeHybrid  SearchMode = "hybrid"
)

// SearchRequest carries everything a leg needs: raw text for the lexical leg,
// the embedding for the vector leg, and how many hits to return.
// An empty Mode means vector, so old fakes keep compiling with vector behaviour.
type SearchRequest struct {
	Query     string
	Embedding []float32
	Limit     int
	Mode      SearchMode
}

// SearchHit is one retrieved chunk plus the provenance the answer must cite.
// Score ranks hits: higher is better (cosine similarity, lexical overlap, RRF).
type SearchHit struct {
	SourcePath  string
	Title       string
	HeadingPath string
	Position    int
	Content     string
	Score       float64
}

// Chat roles.
const (
	RoleSystem = "system"
	RoleUser   = "user"
	RoleTool   = "tool"
)

// Message is one turn of a conversation. ToolCalls is set on an assistant turn
// that asked for tools; ToolName attributes a tool result to its tool.
type Message struct {
	Role      string
	Content   string
	ToolCalls []ToolCall
	ToolName  string
}

// ToolCall is a tool invocation requested by the model. Arguments is the raw JSON
// object the model produced: it is untrusted input and each tool validates it.
type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// ToolSpec is the schema advertised to the model.
type ToolSpec struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ChatRequest is one chat turn plus the tools the model may call.
type ChatRequest struct {
	Messages []Message
	Tools    []ToolSpec
}

// ChatResponse is the model's answer to a turn: visible text and/or tool calls.
type ChatResponse struct {
	Content   string
	ToolCalls []ToolCall
}

// ChatModel streams a completion. emit is called once per delta; returning an
// error from emit aborts the stream and propagates the error.
type ChatModel interface {
	Chat(ctx context.Context, req ChatRequest, emit func(delta string) error) (ChatResponse, error)
}

// Tool is one capability the agent may execute. The set of tools is an allowlist:
// nothing outside it is reachable, whatever the model asks for.
type Tool interface {
	Spec() ToolSpec
	Execute(ctx context.Context, args json.RawMessage) (string, error)
}

// CanvasRepository is the state the agent acts on.
type CanvasRepository interface {
	ListLayers(ctx context.Context) ([]domain.Layer, error)
	SetWiggle(ctx context.Context, layerID string, jiggle, speed int) (domain.Layer, error)
	ToggleLayerVisibility(ctx context.Context, layerID string) (domain.Layer, error)
	ExportProject(ctx context.Context, spec domain.ExportSpec) (domain.ExportResult, error)
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
