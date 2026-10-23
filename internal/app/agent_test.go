package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/r0lm0/doodle-assistant/internal/adapters/memory"
	"github.com/r0lm0/doodle-assistant/internal/app"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

func newAgent(t *testing.T, chat *memory.ChatModel, canvas *memory.Canvas, maxIterations int) *app.RunAgent {
	t.Helper()

	tools, err := app.NewCanvasTools(canvas)
	if err != nil {
		t.Fatalf("NewCanvasTools: %v", err)
	}
	agent, err := app.NewRunAgent(chat, tools, app.RunAgentConfig{MaxIterations: maxIterations})
	if err != nil {
		t.Fatalf("NewRunAgent: %v", err)
	}
	return agent
}

func wiggleCall(id, layer string, jiggle, speed int) ports.ToolCall {
	args, _ := json.Marshal(map[string]any{"layer_id": layer, "jiggle": jiggle, "speed": speed})
	return ports.ToolCall{ID: id, Name: app.ToolSetWiggle, Arguments: args}
}

func TestAgentExecutesToolsAndAnswers(t *testing.T) {
	canvas := testCanvas()
	chat := &memory.ChatModel{Turns: []memory.ChatTurn{
		{ToolCalls: []ports.ToolCall{wiggleCall("call_1", "crayon", 6, 18)}},
		{Deltas: []string{"Listo: ", "dejé el crayon con jiggle 6."}},
	}}
	agent := newAgent(t, chat, canvas, 5)

	var streamed strings.Builder
	result, err := agent.Run(context.Background(), "poné jiggle 6 y velocidad 18 en el crayon", func(delta string) error {
		streamed.WriteString(delta)
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Answer != "Listo: dejé el crayon con jiggle 6." {
		t.Errorf("answer = %q", result.Answer)
	}
	if result.Iterations != 2 {
		t.Errorf("iterations = %d, want 2", result.Iterations)
	}
	if streamed.String() != result.Answer {
		t.Errorf("streamed = %q", streamed.String())
	}
	if len(result.Tools) != 1 || result.Tools[0].Error != "" {
		t.Fatalf("tool traces = %+v", result.Tools)
	}
	if !strings.Contains(result.Tools[0].Result, `"jiggle":6`) {
		t.Errorf("tool result = %s", result.Tools[0].Result)
	}

	layers, _ := canvas.ListLayers(context.Background())
	if layers[1].Jiggle != 6 || layers[1].Speed != 18 {
		t.Errorf("canvas not mutated: %+v", layers[1])
	}

	// The second turn must carry the assistant tool call and the tool result, or
	// the model has no way to know what happened.
	second := chat.Conversations[1]
	var sawAssistantCall, sawToolResult bool
	for _, message := range second {
		if message.Role == "assistant" && len(message.ToolCalls) == 1 {
			sawAssistantCall = true
		}
		if message.Role == ports.RoleTool && message.ToolName == app.ToolSetWiggle && strings.Contains(message.Content, `"jiggle":6`) {
			sawToolResult = true
		}
	}
	if !sawAssistantCall {
		t.Error("the assistant tool call was not echoed back to the model")
	}
	if !sawToolResult {
		t.Error("the tool result was not sent back with role tool")
	}
}

func TestAgentAdvertisesItsToolSet(t *testing.T) {
	chat := &memory.ChatModel{Turns: []memory.ChatTurn{{Deltas: []string{"nada que hacer"}}}}
	agent := newAgent(t, chat, testCanvas(), 5)

	if _, err := agent.Run(context.Background(), "¿qué capas hay?", func(string) error { return nil }); err != nil {
		t.Fatalf("Run: %v", err)
	}

	request, ok := chat.LastRequest()
	if !ok {
		t.Fatal("no request recorded")
	}
	if len(request.Tools) != 4 {
		t.Fatalf("tools advertised = %d, want 4", len(request.Tools))
	}
	names := map[string]bool{}
	for _, spec := range request.Tools {
		names[spec.Name] = true
	}
	for _, want := range []string{app.ToolListLayers, app.ToolSetWiggle, app.ToolToggleLayerVisibility, app.ToolExportProject} {
		if !names[want] {
			t.Errorf("tool %s not advertised", want)
		}
	}
}

func TestAgentRejectsUnknownToolsAndKeepsGoing(t *testing.T) {
	chat := &memory.ChatModel{Turns: []memory.ChatTurn{
		{ToolCalls: []ports.ToolCall{{ID: "x", Name: "rm_rf", Arguments: json.RawMessage(`{"path":"/"}`)}}},
		{Deltas: []string{"no puedo hacer eso"}},
	}}
	agent := newAgent(t, chat, testCanvas(), 5)

	result, err := agent.Run(context.Background(), "borrá todo", func(string) error { return nil })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Tools) != 1 || !strings.Contains(result.Tools[0].Error, "unknown tool") {
		t.Fatalf("trace = %+v", result.Tools)
	}
	if result.Answer != "no puedo hacer eso" {
		t.Errorf("answer = %q", result.Answer)
	}
}

func TestAgentFeedsToolValidationErrorsBack(t *testing.T) {
	canvas := testCanvas()
	chat := &memory.ChatModel{Turns: []memory.ChatTurn{
		{ToolCalls: []ports.ToolCall{wiggleCall("call_1", "crayon", 900, 18)}},
		{Deltas: []string{"el máximo de jiggle es 20"}},
	}}
	agent := newAgent(t, chat, canvas, 5)

	result, err := agent.Run(context.Background(), "poné jiggle 900", func(string) error { return nil })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Tools) != 1 || result.Tools[0].Error == "" {
		t.Fatalf("trace = %+v", result.Tools)
	}

	layers, _ := canvas.ListLayers(context.Background())
	if layers[1].Jiggle != 2 {
		t.Errorf("an invalid tool call mutated the canvas: %+v", layers[1])
	}

	var sawError bool
	for _, message := range chat.Conversations[1] {
		if message.Role == ports.RoleTool && strings.Contains(message.Content, "error") {
			sawError = true
		}
	}
	if !sawError {
		t.Error("the validation error was not returned to the model")
	}
}

func TestAgentStopsAfterMaxIterations(t *testing.T) {
	chat := &memory.ChatModel{Turns: []memory.ChatTurn{
		{ToolCalls: []ports.ToolCall{wiggleCall("c1", "crayon", 1, 12)}},
	}}
	agent := newAgent(t, chat, testCanvas(), 2)

	_, err := agent.Run(context.Background(), "loop infinito", func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "after 2 iterations") {
		t.Fatalf("err = %v, want the iteration limit error", err)
	}
	if chat.CallCount() != 2 {
		t.Errorf("model calls = %d, want exactly the limit", chat.CallCount())
	}
}

func TestAgentPropagatesChatAndEmitErrors(t *testing.T) {
	cause := errors.New("model overloaded")
	agent := newAgent(t, &memory.ChatModel{Err: cause}, testCanvas(), 3)
	if _, err := agent.Run(context.Background(), "hola", func(string) error { return nil }); !errors.Is(err, cause) {
		t.Errorf("chat error not wrapped: %v", err)
	}

	stop := errors.New("client gone")
	agent = newAgent(t, &memory.ChatModel{Turns: []memory.ChatTurn{{Deltas: []string{"a"}}}}, testCanvas(), 3)
	if _, err := agent.Run(context.Background(), "hola", func(string) error { return stop }); !errors.Is(err, stop) {
		t.Errorf("emit error not propagated: %v", err)
	}
}

// stubTool lets the constructor tests build tools with chosen names.
type stubTool struct{ name string }

func (s stubTool) Spec() ports.ToolSpec {
	return ports.ToolSpec{Name: s.name, Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (s stubTool) Execute(context.Context, json.RawMessage) (string, error) { return "{}", nil }

func TestNewRunAgentValidates(t *testing.T) {
	chat := &memory.ChatModel{}
	tools, _ := app.NewCanvasTools(testCanvas())

	if _, err := app.NewRunAgent(nil, tools, app.RunAgentConfig{}); err == nil {
		t.Error("expected an error for a nil chat model")
	}
	if _, err := app.NewRunAgent(chat, nil, app.RunAgentConfig{}); err == nil {
		t.Error("expected an error without tools")
	}
	if _, err := app.NewRunAgent(chat, []ports.Tool{stubTool{"a"}, stubTool{"a"}}, app.RunAgentConfig{}); err == nil {
		t.Error("expected an error for duplicated tool names")
	}
	if _, err := app.NewRunAgent(chat, []ports.Tool{stubTool{""}}, app.RunAgentConfig{}); err == nil {
		t.Error("expected an error for an empty tool name")
	}

	agent, err := app.NewRunAgent(chat, tools, app.RunAgentConfig{})
	if err != nil {
		t.Fatalf("NewRunAgent: %v", err)
	}
	if _, err := agent.Run(context.Background(), "   ", func(string) error { return nil }); err == nil {
		t.Error("expected an error for an empty prompt")
	}
}
