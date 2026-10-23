package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/r0lm0/doodle-assistant/internal/adapters/memory"
	"github.com/r0lm0/doodle-assistant/internal/app"
	"github.com/r0lm0/doodle-assistant/internal/domain"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

func toolByName(t *testing.T, tools []ports.Tool, name string) ports.Tool {
	t.Helper()

	for _, tool := range tools {
		if tool.Spec().Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func testCanvas() *memory.Canvas {
	return memory.NewCanvas(
		domain.Layer{ID: "fondo", Name: "Fondo", Visible: true},
		domain.Layer{ID: "crayon", Name: "Crayon", Visible: true, Jiggle: 2, Speed: 12},
		domain.Layer{ID: "lineart", Name: "Line art", Visible: false, Locked: true},
	)
}

func TestCanvasToolsAdvertiseSchemas(t *testing.T) {
	tools, err := app.NewCanvasTools(testCanvas())
	if err != nil {
		t.Fatalf("NewCanvasTools: %v", err)
	}
	if len(tools) != 4 {
		t.Fatalf("tools = %d, want 4", len(tools))
	}

	for _, tool := range tools {
		spec := tool.Spec()
		if spec.Name == "" || spec.Description == "" {
			t.Errorf("incomplete spec: %+v", spec)
		}
		var schema map[string]any
		if err := json.Unmarshal(spec.Parameters, &schema); err != nil {
			t.Errorf("tool %s has invalid JSON schema: %v", spec.Name, err)
		}
		if schema["type"] != "object" {
			t.Errorf("tool %s schema is not an object: %v", spec.Name, schema["type"])
		}
	}
}

func TestListLayersReturnsTheCanvas(t *testing.T) {
	tools, _ := app.NewCanvasTools(testCanvas())
	out, err := toolByName(t, tools, app.ToolListLayers).Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, `"crayon"`) || !strings.Contains(out, `"jiggle":2`) {
		t.Errorf("output = %s", out)
	}
}

func TestSetWiggleValidatesAndMutates(t *testing.T) {
	canvas := testCanvas()
	tools, _ := app.NewCanvasTools(canvas)
	tool := toolByName(t, tools, app.ToolSetWiggle)

	out, err := tool.Execute(context.Background(), json.RawMessage(`{"layer_id":"crayon","jiggle":6,"speed":18}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, `"jiggle":6`) {
		t.Errorf("output = %s", out)
	}
	layers, _ := canvas.ListLayers(context.Background())
	if layers[1].Jiggle != 6 || layers[1].Speed != 18 {
		t.Errorf("canvas not mutated: %+v", layers[1])
	}

	// The domain rules must reject out-of-range values coming from the model.
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"layer_id":"crayon","jiggle":900,"speed":18}`)); err == nil {
		t.Error("expected an error for jiggle 900")
	}
	// Strict decoding: an invented field is an error, not silently ignored.
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"layer_id":"crayon","jiggle":6,"speed":18,"color":"rojo"}`)); err == nil {
		t.Error("expected an error for an unknown field")
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"layer_id":"no-existe","jiggle":6,"speed":18}`)); !errors.Is(err, domain.ErrLayerNotFound) {
		t.Errorf("missing layer error = %v", err)
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"jiggle":6,"speed":18}`)); err == nil {
		t.Error("expected an error for a missing layer_id")
	}
}

func TestToggleVisibilityAndLockedLayer(t *testing.T) {
	canvas := testCanvas()
	tools, _ := app.NewCanvasTools(canvas)
	tool := toolByName(t, tools, app.ToolToggleLayerVisibility)

	out, err := tool.Execute(context.Background(), json.RawMessage(`{"layer_id":"crayon"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, `"visible":false`) {
		t.Errorf("output = %s", out)
	}

	// Locked layers reject drawing edits but can still be hidden or shown.
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"layer_id":"lineart"}`)); err != nil {
		t.Errorf("toggling a locked layer should work: %v", err)
	}
	if _, err := toolByName(t, tools, app.ToolSetWiggle).Execute(context.Background(), json.RawMessage(`{"layer_id":"lineart","jiggle":3,"speed":12}`)); err == nil {
		t.Error("a locked layer must reject wiggle changes")
	}
}

func TestExportProjectValidatesAndDescribes(t *testing.T) {
	tools, _ := app.NewCanvasTools(testCanvas())
	tool := toolByName(t, tools, app.ToolExportProject)

	out, err := tool.Execute(context.Background(), json.RawMessage(`{"format":"gif","scale":2,"fps":24}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "gif") || !strings.Contains(out, "2x") || !strings.Contains(out, "2 de 3 capas") {
		t.Errorf("output = %s", out)
	}

	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"format":"svg","scale":1,"fps":12}`)); err == nil {
		t.Error("expected an error for an unsupported format")
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"format":"png","scale":3,"fps":12}`)); err == nil {
		t.Error("expected an error for scale 3")
	}
}

func TestNewCanvasToolsRequiresACanvas(t *testing.T) {
	if _, err := app.NewCanvasTools(nil); err == nil {
		t.Error("expected an error for a nil canvas")
	}
}
