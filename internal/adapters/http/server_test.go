package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	httpadapter "github.com/r0lm0/doodle-assistant/internal/adapters/http"
	"github.com/r0lm0/doodle-assistant/internal/app"
	"github.com/r0lm0/doodle-assistant/internal/domain"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

type fakeAsker struct{ err error }

func (f fakeAsker) Ask(_ context.Context, question string, emit func(string) error) (app.AskResult, error) {
	if f.err != nil {
		return app.AskResult{}, f.err
	}
	for _, d := range []string{"El tope ", "es 1920x1080."} {
		if err := emit(d); err != nil {
			return app.AskResult{}, err
		}
	}
	return app.AskResult{
		Answer: domain.Answer{Question: question, Sources: []domain.Source{{SourcePath: "export.md", HeadingPath: "Exportar > Escala", Distance: 0.2, Content: "tope 1920x1080"}}},
	}, nil
}

func (f fakeAsker) AskWithoutContext(_ context.Context, _ string, emit func(string) error) (app.AskResult, error) {
	return app.AskResult{}, emit("sin contexto")
}

type fakeAgentic struct{}

func (fakeAgentic) Ask(_ context.Context, question string, emit func(string) error) (app.AgenticResult, error) {
	if err := emit("respuesta agentica"); err != nil {
		return app.AgenticResult{}, err
	}
	return app.AgenticResult{
		Answer:      domain.Answer{Question: question, Sources: []domain.Source{{SourcePath: "export.md"}}},
		Attempts:    2,
		Queries:     []string{"original", "reformulada"},
		JudgeReason: "ok",
	}, nil
}

type fakeAgent struct{}

func (fakeAgent) Run(_ context.Context, _ string, emit func(string) error) (app.AgentResult, error) {
	if err := emit("listo"); err != nil {
		return app.AgentResult{}, err
	}
	return app.AgentResult{Answer: "listo", Iterations: 2, Tools: []app.ToolTrace{{Iteration: 1, Name: app.ToolSetWiggle, Arguments: "{}", Result: "{}"}}}, nil
}

type fakeStats struct{}

func (fakeStats) Stats(context.Context) (ports.StoreStats, error) {
	return ports.StoreStats{Documents: 8, Chunks: 33}, nil
}

func newServer(t *testing.T) http.Handler {
	t.Helper()

	server, err := httpadapter.NewServer(httpadapter.Deps{
		Asker:   fakeAsker{},
		Agentic: fakeAgentic{},
		Agent:   fakeAgent{},
		Stats:   fakeStats{},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return server.Handler()
}

func doJSON(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestHealthAndStats(t *testing.T) {
	handler := newServer(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/stats", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"chunks":33`) {
		t.Fatalf("stats = %d %s", rec.Code, rec.Body.String())
	}
}

func TestAskStreamsSSEEvents(t *testing.T) {
	rec := doJSON(t, newServer(t), http.MethodPost, "/api/ask", `{"question":"¿tope?","mode":"rag"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("content type = %q", ct)
	}

	body := rec.Body.String()
	for _, want := range []string{"event: delta", "El tope ", `"text":"es 1920x1080."`, "event: sources", "export.md", "event: done", "generate_ms"} {
		if !strings.Contains(body, want) {
			t.Errorf("stream missing %q:\n%s", want, body)
		}
	}

	// Every event must be a well-formed "event:/data:" pair.
	for _, block := range strings.Split(strings.TrimSpace(body), "\n\n") {
		if !strings.HasPrefix(block, "event: ") || !strings.Contains(block, "\ndata: ") {
			t.Errorf("malformed SSE block: %q", block)
		}
	}
}

func TestAskModesAndValidation(t *testing.T) {
	handler := newServer(t)

	rec := doJSON(t, handler, http.MethodPost, "/api/ask", `{"question":"q","mode":"agentic"}`)
	if !strings.Contains(rec.Body.String(), "respuesta agentica") || !strings.Contains(rec.Body.String(), "event: agentic") {
		t.Errorf("agentic stream = %s", rec.Body.String())
	}

	rec = doJSON(t, handler, http.MethodPost, "/api/ask", `{"question":"q","mode":"none"}`)
	if !strings.Contains(rec.Body.String(), "sin contexto") {
		t.Errorf("no-context stream = %s", rec.Body.String())
	}

	rec = doJSON(t, handler, http.MethodPost, "/api/ask", `{"question":"   "}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty question status = %d", rec.Code)
	}

	rec = doJSON(t, handler, http.MethodPost, "/api/ask", `{"question":"q","mode":"raro"}`)
	if !strings.Contains(rec.Body.String(), "unknown mode") {
		t.Errorf("unknown mode stream = %s", rec.Body.String())
	}

	rec = doJSON(t, handler, http.MethodPost, "/api/ask", `{"question":`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid json status = %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"question":"q"}`))
	req.Header.Set("Content-Type", "text/plain")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("wrong content type status = %d", rec.Code)
	}

	big := strings.Repeat("a", 40_000)
	rec = doJSON(t, handler, http.MethodPost, "/api/ask", `{"question":"`+big+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("oversized body status = %d", rec.Code)
	}
}

func TestAgentStreamsDeltasAndTools(t *testing.T) {
	rec := doJSON(t, newServer(t), http.MethodPost, "/api/agent", `{"prompt":"poné jiggle"}`)

	body := rec.Body.String()
	for _, want := range []string{"event: delta", "listo", "event: tools", "set_wiggle", `"iterations":2`} {
		if !strings.Contains(body, want) {
			t.Errorf("agent stream missing %q:\n%s", want, body)
		}
	}

	rec = doJSON(t, newServer(t), http.MethodPost, "/api/agent", `{"prompt":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty prompt status = %d", rec.Code)
	}
}

func TestAskFailureBecomesAnErrorEvent(t *testing.T) {
	server, err := httpadapter.NewServer(httpadapter.Deps{Asker: fakeAsker{err: context.DeadlineExceeded}})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	rec := doJSON(t, server.Handler(), http.MethodPost, "/api/ask", `{"question":"q"}`)
	if !strings.Contains(rec.Body.String(), "event: error") {
		t.Errorf("error stream = %s", rec.Body.String())
	}
}

func TestStaticServesManifestWithItsOwnContentType(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "site.webmanifest"), []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	server, err := httpadapter.NewServer(httpadapter.Deps{WebDir: dir})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/site.webmanifest", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Errorf("manifest content type = %q", ct)
	}

	// The SPA fallback still serves index.html for extension-less paths.
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agente", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "doctype") {
		t.Errorf("spa fallback = %d %q", rec.Code, rec.Body.String())
	}
}

func TestStaticUIReportsMissingBuild(t *testing.T) {
	rec := httptest.NewRecorder()
	newServer(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil || !strings.Contains(payload["error"], "npm") {
		t.Errorf("payload = %s", rec.Body.String())
	}
}
