package ollama_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/r0lm0/doodle-assistant/internal/adapters/ollama"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

func newChat(t *testing.T, url string) *ollama.ChatModel {
	t.Helper()

	chat, err := ollama.NewChatModel(ollama.ChatConfig{BaseURL: url, Model: "gemma-4-e4b"})
	if err != nil {
		t.Fatalf("NewChatModel: %v", err)
	}
	return chat
}

func request(messages ...ports.Message) ports.ChatRequest {
	return ports.ChatRequest{Messages: messages}
}

var conversation = []ports.Message{
	{Role: ports.RoleSystem, Content: "sos un asistente"},
	{Role: ports.RoleUser, Content: "hola"},
}

func TestChatStreamsDeltasInOrder(t *testing.T) {
	var body map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %q, want /api/chat", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/x-ndjson")

		for _, piece := range []string{"Hola", ", ", "mundo"} {
			fmt.Fprintf(w, "{\"message\":{\"role\":\"assistant\",\"content\":%q},\"done\":false}\n", piece)
		}
		fmt.Fprint(w, "{\"message\":{\"role\":\"assistant\",\"content\":\"\"},\"done\":true}\n")
	}))
	defer srv.Close()

	var got strings.Builder
	response, err := newChat(t, srv.URL).Chat(context.Background(), request(conversation...), func(delta string) error {
		got.WriteString(delta)
		return nil
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if got.String() != "Hola, mundo" {
		t.Errorf("streamed text = %q", got.String())
	}
	if response.Content != "Hola, mundo" {
		t.Errorf("returned content = %q", response.Content)
	}
	if len(response.ToolCalls) != 0 {
		t.Errorf("tool calls = %v, want none", response.ToolCalls)
	}
	if body["stream"] != true {
		t.Errorf("request did not ask for streaming: %v", body["stream"])
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Errorf("messages sent = %d, want 2", len(msgs))
	}
	if _, ok := body["tools"]; ok {
		t.Error("tools must be omitted when none are advertised")
	}
}

func TestChatSendsToolSpecsAndParsesToolCalls(t *testing.T) {
	var body map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		fmt.Fprint(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"set_wiggle","arguments":{"layer_id":"crayon","jiggle":6,"speed":12}}}]},"done":false}`+"\n")
		fmt.Fprint(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_9","function":{"name":"list_layers"}}]},"done":false}`+"\n")
		fmt.Fprint(w, `{"message":{"role":"assistant","content":""},"done":true}`+"\n")
	}))
	defer srv.Close()

	req := request(conversation...)
	req.Tools = []ports.ToolSpec{{
		Name:        "set_wiggle",
		Description: "configura el hervido",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}}

	response, err := newChat(t, srv.URL).Chat(context.Background(), req, func(string) error { return nil })
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	tools, _ := body["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("request tools = %d, want 1", len(tools))
	}
	tool, _ := tools[0].(map[string]any)
	function, _ := tool["function"].(map[string]any)
	if function["name"] != "set_wiggle" || function["description"] != "configura el hervido" {
		t.Errorf("tool spec not sent: %v", function)
	}
	if tool["type"] != "function" {
		t.Errorf("tool type = %v, want function", tool["type"])
	}

	if len(response.ToolCalls) != 2 {
		t.Fatalf("tool calls = %d, want 2", len(response.ToolCalls))
	}
	first := response.ToolCalls[0]
	if first.Name != "set_wiggle" {
		t.Errorf("first tool = %q", first.Name)
	}
	if !strings.Contains(string(first.Arguments), `"jiggle":6`) {
		t.Errorf("arguments lost: %s", first.Arguments)
	}
	if first.ID == "" {
		t.Error("a missing tool call id must be synthesized")
	}

	second := response.ToolCalls[1]
	if second.ID != "call_9" {
		t.Errorf("explicit id not preserved: %q", second.ID)
	}
	if string(second.Arguments) != "{}" {
		t.Errorf("missing arguments must become an empty object, got %s", second.Arguments)
	}
}

func TestChatFailsWhenStreamEndsWithoutDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "{\"message\":{\"content\":\"parcial\"},\"done\":false}\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	_, err := newChat(t, srv.URL).Chat(context.Background(), request(conversation...), func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "before done") {
		t.Fatalf("err = %v, want a truncated stream error", err)
	}
}

func TestChatReportsAPIErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"model requires more system memory"}`)
	}))
	defer srv.Close()

	_, err := newChat(t, srv.URL).Chat(context.Background(), request(conversation...), func(string) error { return nil })
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "more system memory") {
		t.Errorf("error lacks status or API message: %v", err)
	}
}

func TestChatAbortsWhenEmitFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 50; i++ {
			fmt.Fprint(w, "{\"message\":{\"content\":\"x\"},\"done\":false}\n")
		}
		fmt.Fprint(w, "{\"message\":{\"content\":\"\"},\"done\":true}\n")
	}))
	defer srv.Close()

	stop := errors.New("client went away")
	calls := 0
	_, err := newChat(t, srv.URL).Chat(context.Background(), request(conversation...), func(string) error {
		calls++
		return stop
	})

	if !errors.Is(err, stop) {
		t.Fatalf("err = %v, want the emit error", err)
	}
	if calls != 1 {
		t.Errorf("emit called %d times, want 1", calls)
	}
}

func TestChatPropagatesCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "{\"message\":{\"content\":\"x\"},\"done\":false}\n")
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newChat(t, srv.URL).Chat(ctx, request(conversation...), func(string) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestChatRequiresMessagesAndConfig(t *testing.T) {
	if _, err := ollama.NewChatModel(ollama.ChatConfig{Model: "m"}); err == nil {
		t.Error("expected an error for an empty base URL")
	}
	if _, err := ollama.NewChatModel(ollama.ChatConfig{BaseURL: "http://localhost:11434"}); err == nil {
		t.Error("expected an error for an empty model")
	}

	chat := newChat(t, "http://127.0.0.1:1")
	if _, err := chat.Chat(context.Background(), ports.ChatRequest{}, func(string) error { return nil }); err == nil {
		t.Error("expected an error for an empty conversation")
	}
}
