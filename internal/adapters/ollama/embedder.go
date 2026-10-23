// Package ollama speaks directly to a local Ollama server with net/http. No SDK:
// the point of this project is to see the wire protocol.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultHTTPTimeout bounds one embedding request. A batch of 32 chunks on a
// 4060 takes well under a second, so 2 minutes means something is wrong.
const defaultHTTPTimeout = 2 * time.Minute

// EmbedderConfig configures the Ollama embedder.
type EmbedderConfig struct {
	BaseURL string
	Model   string

	// HTTPClient is optional; tests inject one with a custom transport.
	HTTPClient *http.Client
}

// Embedder implements ports.Embedder against POST /api/embed.
type Embedder struct {
	url    string
	model  string
	client *http.Client
}

// NewEmbedder validates the configuration and pre-builds the endpoint URL, so a
// broken config fails at startup instead of on the first document.
func NewEmbedder(cfg EmbedderConfig) (*Embedder, error) {
	base := strings.TrimSuffix(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, errors.New("ollama embedder: base URL is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("ollama embedder: model is required")
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}

	return &Embedder{
		url:    base + "/api/embed",
		model:  cfg.Model,
		client: client,
	}, nil
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// apiError is the shape Ollama uses for failures.
type apiError struct {
	Error string `json:"error"`
}

// Embed sends the whole batch in one request. An empty batch is not an HTTP call:
// asking the server to embed nothing would be a wasted round trip.
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	payload, err := json.Marshal(embedRequest{Model: e.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("ollama embed: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("ollama embed: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		// A cancelled context must stay recognizable: callers check errors.Is.
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("ollama embed: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr apiError
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Error != "" {
			return nil, fmt.Errorf("ollama embed: status %d: %s", resp.StatusCode, apiErr.Error)
		}
		return nil, fmt.Errorf("ollama embed: status %d: %s", resp.StatusCode, truncate(body, 200))
	}

	var out embedResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("ollama embed: decode response: %w", err)
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama embed: model returned %d embeddings for %d inputs", len(out.Embeddings), len(texts))
	}

	return out.Embeddings, nil
}

func truncate(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}
