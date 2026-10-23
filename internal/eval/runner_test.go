package eval_test

import (
	"context"
	"testing"

	"github.com/r0lm0/doodle-assistant/internal/adapters/memory"
	"github.com/r0lm0/doodle-assistant/internal/domain"
	"github.com/r0lm0/doodle-assistant/internal/eval"
)

func TestEvaluateEmbedsAllQuestionsInOneBatch(t *testing.T) {
	emb := memory.NewEmbedder(8)
	store := memory.NewVectorStore()

	chunks := domain.ChunkMarkdown("# Capas\n\n## Operaciones\n\nDuplicar copia la capa completa.\n", domain.ChunkOptions{MaxChars: 500, MinChars: 0})
	vectors, err := emb.Embed(context.Background(), []string{chunks[0].EmbeddingText()})
	if err != nil {
		t.Fatalf("seed embeddings: %v", err)
	}
	chunks[0].Embedding = vectors[0]
	if _, err := store.SaveDocument(context.Background(), domain.Document{
		SourcePath: "layers.md", Title: "Capas", ContentHash: "h", Chunks: chunks,
	}); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	path := writeDatasetFile(t, `{"questions":[
		{"id":"q1","question":"¿cómo duplico una capa?","expectedSources":["layers.md"],"expectedHeading":"Operaciones"},
		{"id":"q2","question":"¿cuántas capas hay?","expectedSources":["layers.md"]},
		{"id":"q3","question":"¿algo que no existe?","expectedSources":["nope.md"]}
	]}`)

	runner, err := eval.NewRunner(emb, store)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	before := emb.CallCount()
	report, err := runner.Evaluate(context.Background(), path, []int{1, 5}, true)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if emb.CallCount() != before+1 {
		t.Errorf("embed calls = %d, want exactly one batch for all questions", emb.CallCount()-before)
	}
	if report.Questions != 3 || report.TopK != 5 {
		t.Errorf("report header = %+v", report)
	}
	if len(report.Recall) != 2 {
		t.Errorf("recall keys = %v, want 1 and 5", report.Recall)
	}
	if report.Recall[5] < report.Recall[1] {
		t.Errorf("recall@5 (%v) cannot be lower than recall@1 (%v)", report.Recall[5], report.Recall[1])
	}
	if len(report.Misses) != 1 || report.Misses[0] != "q3" {
		t.Errorf("misses = %v, want [q3]", report.Misses)
	}
	if len(report.Ranks) != 3 {
		t.Errorf("ranks = %v, want one entry per question", report.Ranks)
	}
	if report.Ranks["q3"] != 0 {
		t.Errorf("miss rank for q3 = %d, want 0", report.Ranks["q3"])
	}
	if report.MRR <= 0 || report.MRR > 1 {
		t.Errorf("MRR out of range: %v", report.MRR)
	}
}

func TestEvaluateRejectsBadInput(t *testing.T) {
	runner, err := eval.NewRunner(memory.NewEmbedder(4), memory.NewVectorStore())
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	if _, err := runner.Evaluate(context.Background(), "no-existe.json", []int{3}, false); err == nil {
		t.Error("expected an error for a missing dataset")
	}
	path := writeDatasetFile(t, `{"questions":[{"id":"q1","question":"a","expectedSources":["a.md"]}]}`)
	if _, err := runner.Evaluate(context.Background(), path, nil, false); err == nil {
		t.Error("expected an error without K values")
	}
	if _, err := runner.Evaluate(context.Background(), path, []int{0}, false); err == nil {
		t.Error("expected an error for K=0")
	}
	if _, err := eval.NewRunner(nil, memory.NewVectorStore()); err == nil {
		t.Error("expected an error for a nil embedder")
	}
	if _, err := eval.NewRunner(memory.NewEmbedder(4), nil); err == nil {
		t.Error("expected an error for a nil store")
	}
}
