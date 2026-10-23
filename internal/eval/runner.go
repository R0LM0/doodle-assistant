package eval

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/r0lm0/doodle-assistant/internal/ports"
)

// Retriever turns a question into hits. The agentic retriever implements it too, so
// the same dataset can score single-shot retrieval against search-judge-reformulate.
type Retriever interface {
	Retrieve(ctx context.Context, question string, topK int) ([]ports.SearchHit, error)
}

// Runner evaluates a dataset against the current index.
type Runner struct {
	embedder ports.Embedder
	store    ports.VectorStore
}

func NewRunner(embedder ports.Embedder, store ports.VectorStore) (*Runner, error) {
	if embedder == nil {
		return nil, errors.New("eval: embedder is required")
	}
	if store == nil {
		return nil, errors.New("eval: vector store is required")
	}
	return &Runner{embedder: embedder, store: store}, nil
}

// Evaluate embeds every question in one batch (15 questions in one HTTP call
// instead of 15), retrieves the top K chunks for each and reports recall@K + MRR.
func (r *Runner) Evaluate(ctx context.Context, questionsPath string, ks []int, verbose bool) (Report, error) {
	return r.EvaluateWith(ctx, questionsPath, ks, verbose, nil)
}

// EvaluateWith scores the dataset using an alternative retriever (agentic RAG).
// A non-nil retriever disables the batched plain retrieval.
func (r *Runner) EvaluateWith(ctx context.Context, questionsPath string, ks []int, verbose bool, retriever Retriever) (Report, error) {
	questions, err := LoadQuestions(questionsPath)
	if err != nil {
		return Report{}, err
	}
	if len(ks) == 0 {
		return Report{}, errors.New("eval: at least one K is required")
	}
	for _, k := range ks {
		if k <= 0 {
			return Report{}, fmt.Errorf("eval: K must be positive, got %d", k)
		}
	}
	sort.Ints(ks)
	topK := ks[len(ks)-1]

	results, err := r.retrieveWith(ctx, questions, topK, retriever)
	if err != nil {
		return Report{}, err
	}

	report := Report{
		Questions: len(questions),
		TopK:      topK,
		Recall:    make(map[int]float64, len(ks)),
		MRR:       MRR(results),
		Ranks:     make(map[string]int, len(results)),
		Verbose:   verbose,
	}
	for _, k := range ks {
		report.Recall[k] = RecallAtK(results, k)
	}
	for _, result := range results {
		if !anyRelevant(result) {
			report.Misses = append(report.Misses, result.Question.ID)
		}
		report.Ranks[result.Question.ID] = firstRelevantRank(result)
	}

	return report, nil
}

func (r *Runner) retrieveWith(ctx context.Context, questions []Question, topK int, retriever Retriever) ([]Retrieved, error) {
	if retriever != nil {
		results := make([]Retrieved, len(questions))
		for i, question := range questions {
			hits, err := retriever.Retrieve(ctx, question.Question, topK)
			if err != nil {
				return nil, fmt.Errorf("eval: retrieve for %s: %w", question.ID, err)
			}
			results[i] = Retrieved{Question: question, Hits: hits}
		}
		return results, nil
	}

	return r.retrieve(ctx, questions, topK)
}

func (r *Runner) retrieve(ctx context.Context, questions []Question, topK int) ([]Retrieved, error) {
	texts := make([]string, len(questions))
	for i, question := range questions {
		texts[i] = question.Question
	}

	vectors, err := r.embedder.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("eval: embed questions: %w", err)
	}
	if len(vectors) != len(questions) {
		return nil, fmt.Errorf("eval: embedder returned %d vectors for %d questions", len(vectors), len(questions))
	}

	results := make([]Retrieved, len(questions))
	for i, question := range questions {
		hits, err := r.store.Search(ctx, vectors[i], topK)
		if err != nil {
			return nil, fmt.Errorf("eval: search for %s: %w", question.ID, err)
		}
		results[i] = Retrieved{Question: question, Hits: hits}
	}
	return results, nil
}

func anyRelevant(result Retrieved) bool {
	return firstRelevantRank(result) > 0
}

// firstRelevantRank returns the 1-based rank of the first relevant hit, or 0.
func firstRelevantRank(result Retrieved) int {
	for rank, hit := range result.Hits {
		if IsRelevant(result.Question, hit) {
			return rank + 1
		}
	}
	return 0
}
