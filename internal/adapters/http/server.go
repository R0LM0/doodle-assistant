// Package http exposes the assistant over HTTP with Server-Sent Events.
//
// Streaming is the reason SSE is used instead of a plain JSON response: the user
// sees the answer being written instead of waiting for the whole generation.
package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/r0lm0/doodle-assistant/internal/app"
	"github.com/r0lm0/doodle-assistant/internal/domain"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

// maxBodyBytes bounds every JSON request body.
const maxBodyBytes = 16 << 10

// Asker answers with the classic pipeline.
type Asker interface {
	Ask(ctx context.Context, question string, emit func(delta string) error) (app.AskResult, error)
	AskWithoutContext(ctx context.Context, question string, emit func(delta string) error) (app.AskResult, error)
}

// AgenticAsker answers with search-judge-reformulate retrieval.
type AgenticAsker interface {
	Ask(ctx context.Context, question string, emit func(delta string) error) (app.AgenticResult, error)
}

// AgentRunner runs the tool-calling loop.
type AgentRunner interface {
	Run(ctx context.Context, prompt string, emit func(delta string) error) (app.AgentResult, error)
}

// StatsReader reports what is indexed.
type StatsReader interface {
	Stats(ctx context.Context) (ports.StoreStats, error)
}

// Deps are the handlers' collaborators. Interfaces are declared here so the
// adapter does not depend on the CLI adapter or on concrete implementations.
type Deps struct {
	Asker   Asker
	Agentic AgenticAsker
	Agent   AgentRunner
	Stats   StatsReader
	WebDir  string
	Logger  *slog.Logger
}

// Server is the HTTP adapter.
type Server struct {
	deps Deps
	mux  *http.ServeMux
}

func NewServer(deps Deps) (*Server, error) {
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	s := &Server{deps: deps, mux: http.NewServeMux()}
	s.routes()
	return s, nil
}

// Handler returns the mux, ready for http.Server.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/stats", s.handleStats)
	s.mux.HandleFunc("POST /api/ask", s.handleAsk)
	s.mux.HandleFunc("POST /api/agent", s.handleAgent)
	s.mux.Handle("GET /", s.static())
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if s.deps.Stats == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "stats are not configured")
		return
	}
	stats, err := s.deps.Stats.Stats(r.Context())
	if err != nil {
		s.deps.Logger.Error("stats", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "could not read stats")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"documents": stats.Documents, "chunks": stats.Chunks})
}

type askRequest struct {
	Question string `json:"question"`
	Mode     string `json:"mode"` // rag | agentic | none
}

func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	var req askRequest
	if !decodeBody(w, r, &req) {
		return
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		writeJSONError(w, http.StatusBadRequest, "question is required")
		return
	}

	mode := req.Mode
	if mode == "" {
		mode = "rag"
	}

	stream, ok := newSSE(w)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	emit := func(delta string) error { return stream.event("delta", map[string]string{"text": delta}) }

	switch mode {
	case "none":
		if s.deps.Asker == nil {
			stream.event("error", map[string]string{"message": "no asker configured"})
			return
		}
		result, err := s.deps.Asker.AskWithoutContext(r.Context(), question, emit)
		s.finishAsk(stream, result.Answer.Sources, result.EmbedTime+result.SearchTime, result.GenerateTime, err)
	case "rag":
		if s.deps.Asker == nil {
			stream.event("error", map[string]string{"message": "no asker configured"})
			return
		}
		result, err := s.deps.Asker.Ask(r.Context(), question, emit)
		s.finishAsk(stream, result.Answer.Sources, result.EmbedTime+result.SearchTime, result.GenerateTime, err)
	case "agentic":
		if s.deps.Agentic == nil {
			stream.event("error", map[string]string{"message": "agentic mode is not configured"})
			return
		}
		result, err := s.deps.Agentic.Ask(r.Context(), question, emit)
		s.finishAsk(stream, result.Answer.Sources, result.EmbedTime+result.SearchTime+result.JudgeTime, result.GenerateTime, err)
		_ = stream.event("agentic", map[string]any{
			"attempts": result.Attempts,
			"queries":  result.Queries,
			"reason":   result.JudgeReason,
		})
	default:
		stream.event("error", map[string]string{"message": "unknown mode " + mode})
	}
}

// finishAsk emits the sources and timings, then closes the stream. Errors become
// an SSE event because the headers are already sent: a 500 is no longer possible.
func (s *Server) finishAsk(stream *sse, sources []domain.Source, retrieval, generate time.Duration, err error) {
	if err != nil {
		s.deps.Logger.Error("ask failed", "error", err)
		_ = stream.event("error", map[string]string{"message": err.Error()})
		return
	}

	type source struct {
		SourcePath  string  `json:"source_path"`
		HeadingPath string  `json:"heading_path"`
		Distance    float64 `json:"distance"`
		Content     string  `json:"content"`
	}
	out := make([]source, len(sources))
	for i, src := range sources {
		out[i] = source{SourcePath: src.SourcePath, HeadingPath: src.HeadingPath, Distance: src.Distance, Content: src.Content}
	}

	_ = stream.event("sources", map[string]any{"sources": out})
	_ = stream.event("done", map[string]any{
		"retrieval_ms": retrieval.Milliseconds(),
		"generate_ms":  generate.Milliseconds(),
	})
}

type agentRequest struct {
	Prompt string `json:"prompt"`
}

func (s *Server) handleAgent(w http.ResponseWriter, r *http.Request) {
	var req agentRequest
	if !decodeBody(w, r, &req) {
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		writeJSONError(w, http.StatusBadRequest, "prompt is required")
		return
	}
	if s.deps.Agent == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "agent is not configured")
		return
	}

	stream, ok := newSSE(w)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	result, err := s.deps.Agent.Run(r.Context(), prompt, func(delta string) error {
		return stream.event("delta", map[string]string{"text": delta})
	})
	if err != nil {
		s.deps.Logger.Error("agent failed", "error", err)
		_ = stream.event("error", map[string]string{"message": err.Error()})
		return
	}

	traces := make([]map[string]any, len(result.Tools))
	for i, trace := range result.Tools {
		traces[i] = map[string]any{
			"iteration": trace.Iteration,
			"name":      trace.Name,
			"arguments": trace.Arguments,
			"result":    trace.Result,
			"error":     trace.Error,
		}
	}
	_ = stream.event("tools", map[string]any{"tools": traces})
	_ = stream.event("done", map[string]any{"iterations": result.Iterations})
}

// static serves the built UI and falls back to index.html so the SPA works on
// refresh. A missing build is reported plainly instead of 404s everywhere.
func (s *Server) static() http.Handler {
	dir := s.deps.WebDir
	if dir == "" {
		dir = "web/dist"
	}

	fileServer := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || filepath.Ext(r.URL.Path) == "" {
			index := filepath.Join(dir, "index.html")
			if _, err := os.Stat(index); err != nil {
				writeJSONError(w, http.StatusServiceUnavailable, "UI is not built: run npm install && npm run build in web/")
				return
			}
			http.ServeFile(w, r, index)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeJSONError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return false
	}

	limited := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(limited).Decode(dst); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// sse writes one Server-Sent Event per call.
type sse struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newSSE(w http.ResponseWriter) (*sse, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Disables proxy buffering so deltas arrive as they are produced.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	return &sse{w: w, flusher: flusher}, true
}

func (s *sse) event(name string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode event %s: %w", name, err)
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, body); err != nil {
		return fmt.Errorf("write event %s: %w", name, err)
	}
	s.flusher.Flush()
	return nil
}
