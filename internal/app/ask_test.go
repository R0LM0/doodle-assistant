package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/r0lm0/doodle-assistant/internal/adapters/memory"
	"github.com/r0lm0/doodle-assistant/internal/app"
	"github.com/r0lm0/doodle-assistant/internal/domain"
)

func seededStore(t *testing.T, emb *memory.Embedder) *memory.VectorStore {
	t.Helper()

	store := memory.NewVectorStore()
	doc := domain.Document{
		SourcePath:  "export.md",
		Title:       "Exportar",
		ContentHash: "hash-export",
	}
	chunks := domain.ChunkMarkdown("# Exportar\n\n## Escala\n\nLa escala va de 1x a 2x con tope de 1920x1080.\n\n## Fondo\n\nEl fondo puede ser transparente.\n", domain.ChunkOptions{MaxChars: 1000, MinChars: 0})
	doc.Chunks = chunks

	vectors, err := emb.Embed(context.Background(), []string{chunks[0].EmbeddingText(), chunks[1].EmbeddingText()})
	if err != nil {
		t.Fatalf("seed embeddings: %v", err)
	}
	for i := range doc.Chunks {
		doc.Chunks[i].Embedding = vectors[i]
	}
	if _, err := store.SaveDocument(context.Background(), doc); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	return store
}

func newAsk(t *testing.T, emb *memory.Embedder, store *memory.VectorStore, chat *memory.ChatModel) *app.Ask {
	t.Helper()

	uc, err := app.NewAsk(emb, store, chat, app.AskConfig{TopK: 2})
	if err != nil {
		t.Fatalf("NewAsk: %v", err)
	}
	return uc
}

func TestAskRetrievesStreamsAndCitesSources(t *testing.T) {
	emb := memory.NewEmbedder(8)
	store := seededStore(t, emb)
	chat := &memory.ChatModel{Deltas: []string{"El tope es ", "1920x1080."}}
	uc := newAsk(t, emb, store, chat)

	var streamed strings.Builder
	result, err := uc.Ask(context.Background(), "¿Cuál es el tope de exportación?", func(delta string) error {
		streamed.WriteString(delta)
		return nil
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if streamed.String() != "El tope es 1920x1080." {
		t.Errorf("streamed = %q", streamed.String())
	}
	if result.Answer.Text != streamed.String() {
		t.Errorf("accumulated answer = %q, streamed = %q", result.Answer.Text, streamed.String())
	}
	if result.Retrieved != 2 {
		t.Errorf("retrieved = %d, want 2", result.Retrieved)
	}
	if len(result.Answer.Sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(result.Answer.Sources))
	}
	if result.Answer.Sources[0].Citation() == "" || result.Answer.Sources[0].Content == "" {
		t.Errorf("source lost provenance or content: %+v", result.Answer.Sources[0])
	}

	conversation := chat.LastConversation()
	if len(conversation) != 2 {
		t.Fatalf("conversation = %d messages, want system+user", len(conversation))
	}
	if !strings.Contains(conversation[0].Content, "SOLO") {
		t.Errorf("system message lost the RAG rules: %q", conversation[0].Content)
	}
	if !strings.Contains(conversation[1].Content, "export.md > Exportar > Escala") {
		t.Errorf("user message does not contain the cited chunk: %q", conversation[1].Content)
	}
	if !strings.Contains(conversation[1].Content, "¿Cuál es el tope de exportación?") {
		t.Errorf("user message lost the question: %q", conversation[1].Content)
	}
}

func TestAskWithoutContextSkipsRetrieval(t *testing.T) {
	emb := memory.NewEmbedder(8)
	store := seededStore(t, emb)
	chat := &memory.ChatModel{Deltas: []string{"respuesta sin contexto"}}
	uc := newAsk(t, emb, store, chat)

	before := emb.CallCount()

	result, err := uc.AskWithoutContext(context.Background(), "¿Cuál es el tope de exportación?", func(string) error { return nil })
	if err != nil {
		t.Fatalf("AskWithoutContext: %v", err)
	}

	if emb.CallCount() != before {
		t.Errorf("no-context mode embedded the question: calls %d -> %d", before, emb.CallCount())
	}
	if result.Retrieved != 0 || len(result.Answer.Sources) != 0 {
		t.Errorf("no-context mode returned sources: %+v", result)
	}
	conversation := chat.LastConversation()
	if len(conversation) != 2 || conversation[1].Content != "¿Cuál es el tope de exportación?" {
		t.Errorf("user message should be the bare question: %+v", conversation)
	}
}

func TestAskValidatesTheQuestion(t *testing.T) {
	emb := memory.NewEmbedder(8)
	uc := newAsk(t, emb, seededStore(t, emb), &memory.ChatModel{})
	before := emb.CallCount() // seeding the store already cost one batch

	for _, question := range []string{"", "   ", strings.Repeat("a", 2001)} {
		if _, err := uc.Ask(context.Background(), question, func(string) error { return nil }); err == nil {
			t.Errorf("expected an error for a question of %d chars", len(question))
		}
	}
	if emb.CallCount() != before {
		t.Errorf("an invalid question cost %d extra embedding calls", emb.CallCount()-before)
	}
}

func TestAskWrapsDependencyErrors(t *testing.T) {
	cause := errors.New("boom")

	emb := memory.NewEmbedder(8)
	emb.Err = cause
	uc := newAsk(t, emb, memory.NewVectorStore(), &memory.ChatModel{})
	if _, err := uc.Ask(context.Background(), "pregunta", func(string) error { return nil }); !errors.Is(err, cause) {
		t.Errorf("embedder error not wrapped: %v", err)
	}

	emb = memory.NewEmbedder(8)
	store := memory.NewVectorStore()
	chat := &memory.ChatModel{Err: cause}
	uc = newAsk(t, emb, store, chat)
	if _, err := uc.Ask(context.Background(), "pregunta", func(string) error { return nil }); err == nil {
		t.Error("expected an error when there is no context to search")
	}
}

func TestAskPropagatesChatAndEmitErrors(t *testing.T) {
	emb := memory.NewEmbedder(8)
	chat := &memory.ChatModel{Err: errors.New("model overloaded")}
	uc := newAsk(t, emb, seededStore(t, emb), chat)

	if _, err := uc.Ask(context.Background(), "pregunta", func(string) error { return nil }); err == nil ||
		!strings.Contains(err.Error(), "model overloaded") {
		t.Errorf("chat error not surfaced: %v", err)
	}

	stop := errors.New("client disconnected")
	chat = &memory.ChatModel{Deltas: []string{"una", "dos", "tres"}}
	uc = newAsk(t, emb, seededStore(t, emb), chat)
	if _, err := uc.Ask(context.Background(), "pregunta", func(string) error { return stop }); !errors.Is(err, stop) {
		t.Errorf("emit error not propagated: %v", err)
	}
}

func TestNewAskRejectsMissingDependencies(t *testing.T) {
	emb := memory.NewEmbedder(8)
	store := memory.NewVectorStore()
	chat := &memory.ChatModel{}

	if _, err := app.NewAsk(nil, store, chat, app.AskConfig{}); err == nil {
		t.Error("expected an error for a nil embedder")
	}
	if _, err := app.NewAsk(emb, nil, chat, app.AskConfig{}); err == nil {
		t.Error("expected an error for a nil store")
	}
	if _, err := app.NewAsk(emb, store, nil, app.AskConfig{}); err == nil {
		t.Error("expected an error for a nil chat model")
	}
}
