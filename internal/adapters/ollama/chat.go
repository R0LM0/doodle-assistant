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
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	ToolCalls []chatToolCall `json:"tool_calls,omitempty"`
	ToolName  string         `json:"tool_name,omitempty"`
}

// chatToolCall is how Ollama reports a requested tool: the arguments are a JSON
// object, kept raw so the tool validates them instead of this adapter.
type chatToolCall struct {
	ID       string `json:"id,omitempty"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type chatTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Tools    []chatTool    `json:"tools,omitempty"`
}

// chatStreamChunk is one NDJSON line of the response.
type chatStreamChunk struct {
	Message chatMessage `json:"message"`
	Done    bool        `json:"done"`
	Error   string      `json:"error"`
}

// Chat sends the conversation and the advertised tools, calls emit for every
// content delta, and returns the accumulated text plus any tool calls.
//
// Errors returned by emit abort the stream: a caller that cannot consume the
// answer should stop paying for it.
func (c *ChatModel) Chat(ctx context.Context, req ports.ChatRequest, emit func(delta string) error) (ports.ChatResponse, error) {
	var response ports.ChatResponse

	if len(req.Messages) == 0 {
		return response, errors.New("ollama chat: at least one message is required")
	}

	turns := make([]chatMessage, len(req.Messages))
	for i, m := range req.Messages {
		turns[i] = chatMessage{Role: m.Role, Content: m.Content, ToolName: m.ToolName}
		for _, call := range m.ToolCalls {
			var converted chatToolCall
			converted.ID = call.ID
			converted.Function.Name = call.Name
			converted.Function.Arguments = call.Arguments
			turns[i].ToolCalls = append(turns[i].ToolCalls, converted)
		}
	}

	payload := chatRequest{Model: c.model, Messages: turns, Stream: true}
	for _, spec := range req.Tools {
		var tool chatTool
		tool.Type = "function"
		tool.Function.Name = spec.Name
		tool.Function.Description = spec.Description
		tool.Function.Parameters = spec.Parameters
		payload.Tools = append(payload.Tools, tool)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return response, fmt.Errorf("ollama chat: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return response, fmt.Errorf("ollama chat: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return response, fmt.Errorf("ollama chat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		var apiErr apiError
		if json.Unmarshal(raw, &apiErr) == nil && apiErr.Error != "" {
			return response, fmt.Errorf("ollama chat: status %d: %s", resp.StatusCode, apiErr.Error)
		}
		return response, fmt.Errorf("ollama chat: status %d: %s", resp.StatusCode, truncate(raw, 200))
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
			return response, fmt.Errorf("ollama chat: decode stream line: %w", err)
		}
		if chunk.Error != "" {
			return response, fmt.Errorf("ollama chat: %s", chunk.Error)
		}

		if chunk.Message.Content != "" {
			response.Content += chunk.Message.Content
			if err := emit(chunk.Message.Content); err != nil {
				return response, fmt.Errorf("ollama chat: emit delta: %w", err)
			}
		}

		for _, call := range chunk.Message.ToolCalls {
			id := call.ID
			if id == "" {
				// Some model templates omit the id; the loop needs one to match
				// the result back to the call.
				id = fmt.Sprintf("call_%d", len(response.ToolCalls))
			}
			args := call.Function.Arguments
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}
			response.ToolCalls = append(response.ToolCalls, ports.ToolCall{
				ID:        id,
				Name:      call.Function.Name,
				Arguments: args,
			})
		}

		if chunk.Done {
			return response, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return response, fmt.Errorf("ollama chat: read stream: %w", err)
	}
	// A stream that ends without done=true is a truncated answer, not a success.
	return response, errors.New("ollama chat: stream ended before done")
}
