package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/r0lm0/doodle-assistant/internal/domain"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

const (
	defaultAgenticAttempts = 2
	maxAgenticAttempts     = 4
)

// AgenticConfig configures agentic retrieval.
type AgenticConfig struct {
	TopK        int
	MaxAttempts int
	Logger      *slog.Logger
}

// AgenticAsk answers a question by searching, judging the evidence and, when it is
// not enough, reformulating the query before answering.
type AgenticAsk struct {
	embedder    ports.Embedder
	store       ports.VectorStore
	chat        ports.ChatModel
	judge       *EvidenceJudge
	topK        int
	maxAttempts int
	logger      *slog.Logger
}

// AgenticResult reports what happened, including the queries actually used: with a
// retriever that reformulates, "which query worked" is the interesting part.
type AgenticResult struct {
	Answer       domain.Answer
	Retrieved    int
	Attempts     int
	Queries      []string
	JudgeReason  string
	EmbedTime    time.Duration
	SearchTime   time.Duration
	JudgeTime    time.Duration
	GenerateTime time.Duration
}

func NewAgenticAsk(embedder ports.Embedder, store ports.VectorStore, chat ports.ChatModel, judge *EvidenceJudge, cfg AgenticConfig) (*AgenticAsk, error) {
	if embedder == nil {
		return nil, errors.New("agentic ask: embedder is required")
	}
	if store == nil {
		return nil, errors.New("agentic ask: vector store is required")
	}
	if chat == nil {
		return nil, errors.New("agentic ask: chat model is required")
	}
	if judge == nil {
		return nil, errors.New("agentic ask: evidence judge is required")
	}
	if cfg.TopK <= 0 {
		cfg.TopK = defaultTopK
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaultAgenticAttempts
	}
	if cfg.MaxAttempts > maxAgenticAttempts {
		cfg.MaxAttempts = maxAgenticAttempts
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &AgenticAsk{
		embedder:    embedder,
		store:       store,
		chat:        chat,
		judge:       judge,
		topK:        cfg.TopK,
		maxAttempts: cfg.MaxAttempts,
		logger:      cfg.Logger,
	}, nil
}

// Ask runs the agentic loop and then generates a grounded answer.
func (uc *AgenticAsk) Ask(ctx context.Context, question string, emit func(delta string) error) (AgenticResult, error) {
	var result AgenticResult

	question, err := validateQuestion(question)
	if err != nil {
		return result, err
	}
	result.Answer.Question = question

	hits, err := uc.retrieve(ctx, question, &result)
	if err != nil {
		return result, err
	}

	sources := make([]domain.Source, len(hits))
	for i, hit := range hits {
		sources[i] = domain.Source{
			SourcePath:  hit.SourcePath,
			Title:       hit.Title,
			HeadingPath: hit.HeadingPath,
			Position:    hit.Position,
			Distance:    hit.Distance,
			Content:     hit.Content,
		}
	}
	result.Answer.Sources = sources
	result.Retrieved = len(hits)

	// The prompt always carries the user's original question; the reformulations
	// only improved the evidence.
	generateStart := time.Now()
	_, err = uc.chat.Chat(ctx, ports.ChatRequest{Messages: []ports.Message{
		{Role: ports.RoleSystem, Content: domain.BuildPrompt(question, sources).System},
		{Role: ports.RoleUser, Content: domain.BuildPrompt(question, sources).User},
	}}, func(delta string) error {
		result.Answer.Text += delta
		return emit(delta)
	})
	result.GenerateTime = time.Since(generateStart)
	if err != nil {
		return result, fmt.Errorf("agentic ask: generate answer: %w", err)
	}

	uc.logger.Info("agentic answer",
		"attempts", result.Attempts,
		"retrieved", result.Retrieved,
		"queries", result.Queries,
		"judge_ms", result.JudgeTime.Milliseconds(),
		"generate_ms", result.GenerateTime.Milliseconds(),
	)
	return result, nil
}

// Retrieve is the retrieval half, exposed for the evaluation runner so recall and
// latency can be compared against the single-shot pipeline of phase 2.
func (uc *AgenticAsk) Retrieve(ctx context.Context, question string, topK int) ([]ports.SearchHit, error) {
	var scratch AgenticResult
	previous := uc.topK
	if topK > 0 {
		uc.topK = topK
	}
	defer func() { uc.topK = previous }()
	return uc.retrieve(ctx, question, &scratch)
}

func (uc *AgenticAsk) retrieve(ctx context.Context, question string, result *AgenticResult) ([]ports.SearchHit, error) {
	query := question
	var last []ports.SearchHit

	for attempt := 1; attempt <= uc.maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		embedStart := time.Now()
		hits, err := searchHits(ctx, uc.embedder, uc.store, query, uc.topK)
		if err != nil {
			return nil, fmt.Errorf("agentic ask: %w", err)
		}
		result.EmbedTime += time.Since(embedStart)
		result.SearchTime += time.Since(embedStart)

		result.Attempts = attempt
		result.Queries = append(result.Queries, query)
		last = hits

		judgeStart := time.Now()
		enough, reason, err := uc.judge.Judge(ctx, question, hits)
		result.JudgeTime += time.Since(judgeStart)
		result.JudgeReason = reason
		if err != nil {
			return nil, err
		}

		if enough {
			return hits, nil
		}
		if attempt == uc.maxAttempts {
			break
		}

		better, err := uc.judge.Reformulate(ctx, question, hits)
		if err != nil {
			uc.logger.Warn("reformulation failed, answering with what we have", "error", err)
			break
		}
		uc.logger.Info("evidence was insufficient, reformulating", "reason", reason, "query", better)
		query = better
	}

	return last, nil
}
