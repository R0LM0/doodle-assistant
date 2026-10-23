package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/r0lm0/doodle-assistant/internal/domain"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

const (
	defaultTopK = 4
	// maxQuestionChars bounds the input before it reaches the embedder and the
	// model: an unbounded prompt is a cost and injection surface.
	maxQuestionChars = 2000
)

// AskConfig configures the RAG use case.
type AskConfig struct {
	TopK   int
	Logger *slog.Logger
}

// Ask answers a question from the indexed knowledge base.
type Ask struct {
	embedder ports.Embedder
	store    ports.VectorStore
	chat     ports.ChatModel
	topK     int
	logger   *slog.Logger
}

// AskResult carries the answer plus the stage timings the brief asks to expose,
// so it is visible where the latency goes.
type AskResult struct {
	Answer       domain.Answer
	Retrieved    int
	EmbedTime    time.Duration
	SearchTime   time.Duration
	GenerateTime time.Duration
}

func NewAsk(embedder ports.Embedder, store ports.VectorStore, chat ports.ChatModel, cfg AskConfig) (*Ask, error) {
	if embedder == nil {
		return nil, errors.New("ask: embedder is required")
	}
	if store == nil {
		return nil, errors.New("ask: vector store is required")
	}
	if chat == nil {
		return nil, errors.New("ask: chat model is required")
	}
	if cfg.TopK <= 0 {
		cfg.TopK = defaultTopK
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Ask{embedder: embedder, store: store, chat: chat, topK: cfg.TopK, logger: cfg.Logger}, nil
}

// Ask runs the classic RAG pipeline: embed, retrieve, prompt, stream.
func (uc *Ask) Ask(ctx context.Context, question string, emit func(delta string) error) (AskResult, error) {
	var result AskResult

	question, err := validateQuestion(question)
	if err != nil {
		return result, err
	}

	embedStart := time.Now()
	vectors, err := uc.embedder.Embed(ctx, []string{question})
	if err != nil {
		return result, fmt.Errorf("ask: embed question: %w", err)
	}
	if len(vectors) != 1 || len(vectors[0]) == 0 {
		return result, fmt.Errorf("ask: embedder returned %d vectors for one question", len(vectors))
	}
	result.EmbedTime = time.Since(embedStart)

	searchStart := time.Now()
	hits, err := uc.store.Search(ctx, vectors[0], uc.topK)
	if err != nil {
		return result, fmt.Errorf("ask: search chunks: %w", err)
	}
	result.SearchTime = time.Since(searchStart)
	result.Retrieved = len(hits)

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
	result.Answer.Question = question
	result.Answer.Sources = sources

	prompt := domain.BuildPrompt(question, sources)
	result.GenerateTime, err = uc.generate(ctx, prompt, emit, &result.Answer.Text)
	if err != nil {
		return result, err
	}

	uc.logger.Info("answered question",
		"retrieved", result.Retrieved,
		"embed_ms", result.EmbedTime.Milliseconds(),
		"search_ms", result.SearchTime.Milliseconds(),
		"generate_ms", result.GenerateTime.Milliseconds(),
	)
	return result, nil
}

// AskWithoutContext is the control group of the experiment: same model, same
// question, no retrieval. Comparing the two answers is the best way to see what
// RAG actually adds (and where it lies).
func (uc *Ask) AskWithoutContext(ctx context.Context, question string, emit func(delta string) error) (AskResult, error) {
	var result AskResult

	question, err := validateQuestion(question)
	if err != nil {
		return result, err
	}

	result.Answer.Question = question
	result.GenerateTime, err = uc.generate(ctx, domain.NoContextPrompt(question), emit, &result.Answer.Text)
	if err != nil {
		return result, err
	}
	return result, nil
}

// generate maps the domain prompt to transport messages, streams the deltas to
// the caller and accumulates the text.
func (uc *Ask) generate(ctx context.Context, prompt domain.Prompt, emit func(delta string) error, text *string) (time.Duration, error) {
	messages := []ports.Message{
		{Role: ports.RoleSystem, Content: prompt.System},
		{Role: ports.RoleUser, Content: prompt.User},
	}

	start := time.Now()
	_, err := uc.chat.Chat(ctx, ports.ChatRequest{Messages: messages}, func(delta string) error {
		*text += delta
		return emit(delta)
	})
	took := time.Since(start)
	if err != nil {
		return took, fmt.Errorf("ask: generate answer: %w", err)
	}
	return took, nil
}

func validateQuestion(question string) (string, error) {
	trimmed := strings.TrimSpace(question)
	if trimmed == "" {
		return "", errors.New("ask: question must not be empty")
	}
	if len(trimmed) > maxQuestionChars {
		return "", fmt.Errorf("ask: question is too long (%d chars, max %d)", len(trimmed), maxQuestionChars)
	}
	return trimmed, nil
}
