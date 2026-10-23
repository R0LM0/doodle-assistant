package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/r0lm0/doodle-assistant/internal/ports"
)

// EvidenceJudge asks the model, in a separate call without tools, whether the
// retrieved fragments are enough to answer the question. It is the agentic part of
// agentic RAG: retrieval stops being "always one shot".
type EvidenceJudge struct {
	chat ports.ChatModel
}

func NewEvidenceJudge(chat ports.ChatModel) (*EvidenceJudge, error) {
	if chat == nil {
		return nil, errors.New("evidence judge: chat model is required")
	}
	return &EvidenceJudge{chat: chat}, nil
}

const judgePrompt = `Sos un evaluador de evidencia. Te doy una pregunta y fragmentos de documentación.
Respondé SOLO con una primera línea que diga SI o NO (¿la evidencia alcanza para responder con precisión?) y una segunda línea con el motivo en menos de 15 palabras.
No agregues nada más.`

// Judge returns whether the evidence suffices plus the model's reason.
func (j *EvidenceJudge) Judge(ctx context.Context, question string, hits []ports.SearchHit) (bool, string, error) {
	var evidence strings.Builder
	for i, hit := range hits {
		fmt.Fprintf(&evidence, "[%d] %s > %s\n%s\n\n", i+1, hit.SourcePath, hit.HeadingPath, hit.Content)
	}
	if len(hits) == 0 {
		evidence.WriteString("(sin fragmentos)")
	}

	request := ports.ChatRequest{Messages: []ports.Message{
		{Role: ports.RoleSystem, Content: judgePrompt},
		{Role: ports.RoleUser, Content: "PREGUNTA:\n" + question + "\n\nEVIDENCIA:\n" + evidence.String()},
	}}

	var answer strings.Builder
	response, err := j.chat.Chat(ctx, request, func(delta string) error {
		answer.WriteString(delta)
		return nil
	})
	if err != nil {
		return false, "", fmt.Errorf("evidence judge: %w", err)
	}

	text := answer.String()
	if text == "" {
		text = response.Content
	}
	return parseVerdict(text)
}

// parseVerdict reads the SI/NO first line. Anything that is not a clear SI counts
// as "not enough": a judge that fails open would answer from weak evidence.
func parseVerdict(text string) (bool, string, error) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return false, "", errors.New("evidence judge: empty verdict")
	}

	first := strings.ToUpper(strings.TrimSpace(lines[0]))
	reason := ""
	if len(lines) > 1 {
		reason = strings.TrimSpace(strings.Join(lines[1:], " "))
	}

	switch {
	case strings.HasPrefix(first, "SI") || strings.HasPrefix(first, "SÍ"):
		return true, reason, nil
	default:
		return false, reason, nil
	}
}

const reformulatePrompt = `Sos un buscador. Te doy una pregunta y fragmentos que no alcanzaron.
Escribí UNA sola consulta de búsqueda alternativa, más específica, usando palabras que aparecerían en la documentación.
Respondé solo con la consulta, sin comillas ni explicación.`

// Reformulate asks for a better query. The result is trimmed to one line because
// models like to explain themselves even when told not to.
func (j *EvidenceJudge) Reformulate(ctx context.Context, question string, hits []ports.SearchHit) (string, error) {
	var seen strings.Builder
	for _, hit := range hits {
		fmt.Fprintf(&seen, "- %s > %s\n", hit.SourcePath, hit.HeadingPath)
	}

	request := ports.ChatRequest{Messages: []ports.Message{
		{Role: ports.RoleSystem, Content: reformulatePrompt},
		{Role: ports.RoleUser, Content: "PREGUNTA:\n" + question + "\n\nYA REVISADO:\n" + seen.String()},
	}}

	response, err := j.chat.Chat(ctx, request, func(string) error { return nil })
	if err != nil {
		return "", fmt.Errorf("reformulate: %w", err)
	}

	query := strings.TrimSpace(response.Content)
	if i := strings.IndexByte(query, '\n'); i >= 0 {
		query = strings.TrimSpace(query[:i])
	}
	query = strings.Trim(query, "\"'`")
	if query == "" {
		return "", errors.New("reformulate: model returned an empty query")
	}
	if len(query) > maxQuestionChars {
		query = query[:maxQuestionChars]
	}
	return query, nil
}
