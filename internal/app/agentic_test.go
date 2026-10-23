package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/r0lm0/doodle-assistant/internal/adapters/memory"
	"github.com/r0lm0/doodle-assistant/internal/app"
)

func newAgentic(t *testing.T, emb *memory.Embedder, store *memory.VectorStore, chat, judgeChat *memory.ChatModel, attempts int) *app.AgenticAsk {
	t.Helper()

	judge, err := app.NewEvidenceJudge(judgeChat)
	if err != nil {
		t.Fatalf("NewEvidenceJudge: %v", err)
	}
	uc, err := app.NewAgenticAsk(emb, store, chat, judge, app.AgenticConfig{TopK: 2, MaxAttempts: attempts})
	if err != nil {
		t.Fatalf("NewAgenticAsk: %v", err)
	}
	return uc
}

func TestAgenticAskStopsWhenEvidenceIsEnough(t *testing.T) {
	emb := memory.NewEmbedder(8)
	store := seededStore(t, emb)
	chat := &memory.ChatModel{Deltas: []string{"El tope es 1920x1080."}}
	judgeChat := &memory.ChatModel{Deltas: []string{"SI\nalcanza"}}
	uc := newAgentic(t, emb, store, chat, judgeChat, 2)

	result, err := uc.Ask(context.Background(), "¿Cuál es el tope?", func(string) error { return nil })
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if result.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", result.Attempts)
	}
	if len(result.Queries) != 1 || result.Queries[0] != "¿Cuál es el tope?" {
		t.Errorf("queries = %v", result.Queries)
	}
	if result.Retrieved != 2 || len(result.Answer.Sources) != 2 {
		t.Errorf("retrieved = %d, sources = %d", result.Retrieved, len(result.Answer.Sources))
	}
	if result.Answer.Text != "El tope es 1920x1080." {
		t.Errorf("answer = %q", result.Answer.Text)
	}
	if judgeChat.CallCount() != 1 {
		t.Errorf("judge calls = %d, want 1", judgeChat.CallCount())
	}
}

func TestAgenticAskReformulatesWhenEvidenceIsWeak(t *testing.T) {
	emb := memory.NewEmbedder(8)
	store := seededStore(t, emb)
	chat := &memory.ChatModel{Deltas: []string{"respuesta con evidencia reformulada"}}
	judgeChat := &memory.ChatModel{Turns: []memory.ChatTurn{
		{Deltas: []string{"NO\nno habla del tope"}},
		{Deltas: []string{"tope de exportacion 1920"}},
		{Deltas: []string{"SI\nahora si"}},
	}}
	uc := newAgentic(t, emb, store, chat, judgeChat, 2)

	result, err := uc.Ask(context.Background(), "¿hasta cuánto exporta?", func(string) error { return nil })
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if result.Attempts != 2 {
		t.Errorf("attempts = %d, want 2", result.Attempts)
	}
	if len(result.Queries) != 2 || result.Queries[1] != "tope de exportacion 1920" {
		t.Errorf("queries = %v, want the reformulated one second", result.Queries)
	}
	if emb.CallCount() != 3 { // 1 seeding + 2 retrieval attempts
		t.Errorf("embed calls = %d, want 3", emb.CallCount())
	}
	if !strings.Contains(result.JudgeReason, "ahora si") {
		t.Errorf("judge reason = %q", result.JudgeReason)
	}
}

func TestAgenticAskAnswersAfterExhaustingAttempts(t *testing.T) {
	emb := memory.NewEmbedder(8)
	store := seededStore(t, emb)
	chat := &memory.ChatModel{Deltas: []string{"No encontré eso en la documentación."}}
	judgeChat := &memory.ChatModel{Deltas: []string{"NO\nfalta"}}
	uc := newAgentic(t, emb, store, chat, judgeChat, 2)

	result, err := uc.Ask(context.Background(), "¿algo inexistente?", func(string) error { return nil })
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if result.Attempts != 2 {
		t.Errorf("attempts = %d, want the limit", result.Attempts)
	}
	if result.Answer.Text == "" {
		t.Error("the answer must still be generated with the best evidence available")
	}
}

func TestAgenticAskValidatesAndWrapsErrors(t *testing.T) {
	emb := memory.NewEmbedder(8)
	store := seededStore(t, emb)

	uc := newAgentic(t, emb, store, &memory.ChatModel{}, &memory.ChatModel{}, 2)
	if _, err := uc.Ask(context.Background(), "  ", func(string) error { return nil }); err == nil {
		t.Error("expected an error for an empty question")
	}

	cause := errors.New("judge exploded")
	judgeChat := &memory.ChatModel{Err: cause}
	uc = newAgentic(t, emb, store, &memory.ChatModel{}, judgeChat, 2)
	if _, err := uc.Ask(context.Background(), "pregunta", func(string) error { return nil }); !errors.Is(err, cause) {
		t.Errorf("judge error not wrapped: %v", err)
	}

	if _, err := app.NewAgenticAsk(nil, store, &memory.ChatModel{}, nil, app.AgenticConfig{}); err == nil {
		t.Error("expected an error for a nil embedder or judge")
	}
}

func TestAgenticRetrieveExposesTheLoopForEvaluation(t *testing.T) {
	emb := memory.NewEmbedder(8)
	store := seededStore(t, emb)
	judgeChat := &memory.ChatModel{Deltas: []string{"SI\nok"}}
	uc := newAgentic(t, emb, store, &memory.ChatModel{}, judgeChat, 2)

	hits, err := uc.Retrieve(context.Background(), "¿tope de exportación?", 1)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("hits = %d, want 1 (topK honoured)", len(hits))
	}
}
