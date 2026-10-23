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
	err := newChat(t, srv.URL).Chat(context.Background(), conversation, func(delta string) error {
		got.WriteString(delta)
		return nil
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if got.String() != "Hola, mundo" {
		t.Errorf("streamed text = %q", got.String())
	}
	if body["stream"] != true {
		t.Errorf("request did not ask for streaming: %v", body["stream"])
	}
	if body["model"] != "gemma-4-e4b" {
		t.Errorf("model = %v", body["model"])
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Errorf("messages sent = %d, want 2", len(msgs))
	}
}

func TestChatFailsWhenStreamEndsWithoutDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "{\"message\":{\"content\":\"parcial\"},\"done\":false}\n")
		if flusher != nil {
			flusher.Flush()
		}
		// Return without done=true: the connection closes mid-answer.
	}))
	defer srv.Close()

	err := newChat(t, srv.URL).Chat(context.Background(), conversation, func(string) error { return nil })
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

	err := newChat(t, srv.URL).Chat(context.Background(), conversation, func(string) error { return nil })
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
			fmt.Fprintf(w, "{\"message\":{\"content\":\"x\"},\"done\":false}\n")
		}
		fmt.Fprint(w, "{\"message\":{\"content\":\"\"},\"done\":true}\n")
	}))
	defer srv.Close()

	stop := errors.New("client went away")
	calls := 0
	err := newChat(t, srv.URL).Chat(context.Background(), conversation, func(string) error {
		calls++
		return stop
	})

	if !errors.Is(err, stop) {
		t.Fatalf("err = %v, want the emit error", err)
	}
	if calls != 1 {
		t.Errorf("emit called %d times, want 1 (must stop at the first failure)", calls)
	}
}

func TestChatPropagatesCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "{\"message\":{\"content\":\"x\"},\"done\":false}\n")
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := newChat(t, srv.URL).Chat(ctx, conversation, func(string) error { return nil })
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
	if err := chat.Chat(context.Background(), nil, func(string) error { return nil }); err == nil {
		t.Error("expected an error for an empty conversation")
	}
}
