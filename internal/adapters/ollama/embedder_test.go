package ollama_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/r0lm0/doodle-assistant/internal/adapters/ollama"
)

func newEmbedder(t *testing.T, url string) *ollama.Embedder {
	t.Helper()

	emb, err := ollama.NewEmbedder(ollama.EmbedderConfig{BaseURL: url, Model: "embeddinggemma"})
	if err != nil {
		t.Fatalf("NewEmbedder: %v", err)
	}
	return emb
}

func TestEmbedSendsBatchAndParsesVectors(t *testing.T) {
	var (
		gotPath  string
		gotModel string
		gotInput []string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content type = %q", ct)
		}

		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		gotModel, gotInput = req.Model, req.Input

		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":      req.Model,
			"embeddings": [][]float32{{0.1, 0.2, 0.3}, {0.4, 0.5, 0.6}},
		})
	}))
	defer srv.Close()

	vectors, err := newEmbedder(t, srv.URL).Embed(context.Background(), []string{"uno", "dos"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if gotPath != "/api/embed" {
		t.Errorf("path = %q, want /api/embed", gotPath)
	}
	if gotModel != "embeddinggemma" {
		t.Errorf("model = %q", gotModel)
	}
	if len(gotInput) != 2 || gotInput[0] != "uno" || gotInput[1] != "dos" {
		t.Errorf("input = %v", gotInput)
	}
	if len(vectors) != 2 || len(vectors[0]) != 3 {
		t.Fatalf("vectors = %v", vectors)
	}
	if vectors[1][2] != 0.6 {
		t.Errorf("vector[1][2] = %v, want 0.6", vectors[1][2])
	}
}

func TestEmbedEmptyBatchMakesNoRequest(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	}))
	defer srv.Close()

	vectors, err := newEmbedder(t, srv.URL).Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if vectors != nil {
		t.Errorf("vectors = %v, want nil", vectors)
	}
	if calls != 0 {
		t.Errorf("server was called %d times for an empty batch", calls)
	}
}

func TestEmbedReportsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"model \"nope\" not found"}`))
	}))
	defer srv.Close()

	_, err := newEmbedder(t, srv.URL).Embed(context.Background(), []string{"uno"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), "404") {
		t.Errorf("error should carry status and API message: %v", err)
	}
}

func TestEmbedDetectsVectorCountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{0.1, 0.2}}})
	}))
	defer srv.Close()

	_, err := newEmbedder(t, srv.URL).Embed(context.Background(), []string{"uno", "dos"})
	if err == nil || !strings.Contains(err.Error(), "1 embeddings for 2 inputs") {
		t.Fatalf("err = %v, want a count mismatch error", err)
	}
}

func TestEmbedRejectsMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"embeddings": "no soy una matriz"}`))
	}))
	defer srv.Close()

	_, err := newEmbedder(t, srv.URL).Embed(context.Background(), []string{"uno"})
	if err == nil {
		t.Fatal("expected a decode error")
	}
}

func TestEmbedPropagatesContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{0.1}}})
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newEmbedder(t, srv.URL).Embed(ctx, []string{"uno"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestNewEmbedderValidatesConfig(t *testing.T) {
	if _, err := ollama.NewEmbedder(ollama.EmbedderConfig{Model: "m"}); err == nil {
		t.Error("expected an error for an empty base URL")
	}
	if _, err := ollama.NewEmbedder(ollama.EmbedderConfig{BaseURL: "http://localhost:11434"}); err == nil {
		t.Error("expected an error for an empty model")
	}
}
