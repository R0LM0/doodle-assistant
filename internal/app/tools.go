package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/r0lm0/doodle-assistant/internal/domain"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

// Tool names are constants because they form an allowlist, not free text.
const (
	ToolListLayers            = "list_layers"
	ToolSetWiggle             = "set_wiggle"
	ToolToggleLayerVisibility = "toggle_layer_visibility"
	ToolExportProject         = "export_project"
)

// NewCanvasTools builds the canvas tool set. Everything the agent can do lives
// here: a model cannot invent a fifth tool, because dispatch is a map lookup.
func NewCanvasTools(canvas ports.CanvasRepository) ([]ports.Tool, error) {
	if canvas == nil {
		return nil, errors.New("canvas tools: canvas repository is required")
	}
	return []ports.Tool{
		listLayersTool{canvas: canvas},
		setWiggleTool{canvas: canvas},
		toggleVisibilityTool{canvas: canvas},
		exportProjectTool{canvas: canvas},
	}, nil
}

type listLayersTool struct{ canvas ports.CanvasRepository }

func (listLayersTool) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        ToolListLayers,
		Description: "Lista las capas del canvas con su id, nombre, visibilidad, bloqueo y parámetros de hervido. Usala antes de modificar una capa si no conocés su id.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	}
}

func (t listLayersTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if err := decodeArgs(args, &emptyArgs{}); err != nil {
		return "", err
	}
	layers, err := t.canvas.ListLayers(ctx)
	if err != nil {
		return "", fmt.Errorf("list_layers: %w", err)
	}
	return toJSON(struct {
		Layers []layerResource `json:"layers"`
	}{Layers: toLayerResources(layers)})
}

type setWiggleTool struct{ canvas ports.CanvasRepository }

func (setWiggleTool) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        ToolSetWiggle,
		Description: "Configura el hervido de una capa. jiggle es la amplitud en píxeles (0 a 20) y speed la velocidad en variaciones por segundo (1 a 24).",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"layer_id":{"type":"string","description":"id de la capa, tal como lo devuelve list_layers"},
				"jiggle":{"type":"integer","minimum":0,"maximum":20},
				"speed":{"type":"integer","minimum":1,"maximum":24}
			},
			"required":["layer_id","jiggle","speed"],
			"additionalProperties":false
		}`),
	}
}

func (t setWiggleTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		LayerID string `json:"layer_id"`
		Jiggle  int    `json:"jiggle"`
		Speed   int    `json:"speed"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	if err := domain.ValidateWiggle(in.Jiggle, in.Speed); err != nil {
		return "", err
	}

	layer, err := t.canvas.SetWiggle(ctx, in.LayerID, in.Jiggle, in.Speed)
	if err != nil {
		return "", fmt.Errorf("set_wiggle: %w", err)
	}
	return toJSON(struct {
		Layer layerResource `json:"layer"`
	}{Layer: toLayerResource(layer)})
}

type toggleVisibilityTool struct{ canvas ports.CanvasRepository }

func (toggleVisibilityTool) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        ToolToggleLayerVisibility,
		Description: "Muestra u oculta una capa, invirtiendo su visibilidad actual. Las capas ocultas no se exportan.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{"layer_id":{"type":"string","description":"id de la capa"}},
			"required":["layer_id"],
			"additionalProperties":false
		}`),
	}
}

func (t toggleVisibilityTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		LayerID string `json:"layer_id"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}

	layer, err := t.canvas.ToggleLayerVisibility(ctx, in.LayerID)
	if err != nil {
		return "", fmt.Errorf("toggle_layer_visibility: %w", err)
	}
	return toJSON(struct {
		Layer layerResource `json:"layer"`
	}{Layer: toLayerResource(layer)})
}

type exportProjectTool struct{ canvas ports.CanvasRepository }

func (exportProjectTool) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        ToolExportProject,
		Description: "Simula exportar el proyecto. format: png, gif o webm. scale: 1 o 2. fps: 1 a 60. Devuelve la descripción de lo que se exportaría.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"format":{"type":"string","enum":["png","gif","webm"]},
				"scale":{"type":"integer","enum":[1,2]},
				"fps":{"type":"integer","minimum":1,"maximum":60}
			},
			"required":["format","scale","fps"],
			"additionalProperties":false
		}`),
	}
}

func (t exportProjectTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Format string `json:"format"`
		Scale  int    `json:"scale"`
		FPS    int    `json:"fps"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}

	spec, err := domain.NewExportSpec(in.Format, in.Scale, in.FPS)
	if err != nil {
		return "", err
	}

	result, err := t.canvas.ExportProject(ctx, spec)
	if err != nil {
		return "", fmt.Errorf("export_project: %w", err)
	}
	return toJSON(struct {
		Result string `json:"result"`
	}{Result: result.Description})
}

// layerResource is how a layer is shown to the model: the same snake_case field
// names the tools accept, so a result can be fed straight back as an argument.
// The domain type stays free of JSON tags.
type layerResource struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Visible bool   `json:"visible"`
	Locked  bool   `json:"locked"`
	Jiggle  int    `json:"jiggle"`
	Speed   int    `json:"speed"`
}

func toLayerResource(layer domain.Layer) layerResource {
	return layerResource{
		ID:      layer.ID,
		Name:    layer.Name,
		Visible: layer.Visible,
		Locked:  layer.Locked,
		Jiggle:  layer.Jiggle,
		Speed:   layer.Speed,
	}
}

func toLayerResources(layers []domain.Layer) []layerResource {
	out := make([]layerResource, len(layers))
	for i, layer := range layers {
		out[i] = toLayerResource(layer)
	}
	return out
}

// emptyArgs is the shape of a tool that takes no arguments; decoding into it with
// unknown fields disallowed rejects anything the model invents.
type emptyArgs struct{}

// decodeArgs parses model-produced JSON strictly. The arguments come from a text
// model, so a typo must be an error and not a silently ignored field.
func decodeArgs(args json.RawMessage, dst any) error {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

func toJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode tool result: %w", err)
	}
	return string(encoded), nil
}
