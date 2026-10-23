package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/r0lm0/doodle-assistant/internal/ports"
)

// chatTimeout bounds a whole streamed answer. Lectura por stream no puede usar un
// timeout corto: una respuesta larga tarda, y el que manda de verdad es el ctx.
const chatTimeout = 5 * time.Minute

// ChatConfig configures the Ollama chat model.
type ChatConfig struct {
	BaseURL string
	Model   string

	HTTPClient *http.Client
}

// ChatModel implements ports.ChatModel against POST /api/chat with stream=true.
type ChatModel struct {
	url    string
	model  string
	client *http.Client
}

func NewChatModel(cfg ChatConfig) (*ChatModel, error) {
	base := strings.TrimSuffix(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, errors.New("ollama chat: base URL is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("ollama chat: model is required")
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: chatTimeout}
	}

	return &ChatModel{url: base + "/api/chat", model: cfg.Model, client: client}, nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// chatStreamChunk is one NDJSON line of the response.
type chatStreamChunk struct {
	Message chatMessage `json:"message"`
	Done    bool        `json:"done"`
	Error   string      `json:"error"`
}

// Chat sends the conversation and calls emit for every content delta. Errors
// returned by emit abort the stream and are propagated wrapped, so a caller can
// stop generating (for example because the client disconnected) without reading
// the rest of the answer.
func (c *ChatModel) Chat(ctx context.Context, messages []ports.Message, emit func(delta string) error) error {
	if len(messages) == 0 {
		return errors.New("ollama chat: at least one message is required")
	}

	turns := make([]chatMessage, len(messages))
	for i, m := range messages {
		turns[i] = chatMessage{Role: m.Role, Content: m.Content}
	}

	payload, err := json.Marshal(chatRequest{Model: c.model, Messages: turns, Stream: true})
	if err != nil {
		return fmt.Errorf("ollama chat: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("ollama chat: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("ollama chat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		var apiErr apiError
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Error != "" {
			return fmt.Errorf("ollama chat: status %d: %s", resp.StatusCode, apiErr.Error)
		}
		return fmt.Errorf("ollama chat: status %d: %s", resp.StatusCode, truncate(body, 200))
	}

	// NDJSON: one JSON object per line, no wrapping array. A 4MB line cap is far
	// above any real delta but protects against a runaway response.
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var chunk chatStreamChunk
		if err := json.Unmarshal(line, &chunk); err != nil {
			return fmt.Errorf("ollama chat: decode stream line: %w", err)
		}
		if chunk.Error != "" {
			return fmt.Errorf("ollama chat: %s", chunk.Error)
		}
		if chunk.Message.Content != "" {
			if err := emit(chunk.Message.Content); err != nil {
				return fmt.Errorf("ollama chat: emit delta: %w", err)
			}
		}
		if chunk.Done {
			return nil
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("ollama chat: read stream: %w", err)
	}
	// A stream that ends without done=true is a truncated answer, not a success.
	return errors.New("ollama chat: stream ended before done")
}
