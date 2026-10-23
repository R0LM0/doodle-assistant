package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/r0lm0/doodle-assistant/internal/domain"
	"github.com/r0lm0/doodle-assistant/internal/ports"
)

var _ ports.CanvasRepository = (*Canvas)(nil)

// Canvas is the in-memory canvas the agent acts on. It validates through the
// domain rules, so a fake and the real thing reject the same inputs.
type Canvas struct {
	mu     sync.Mutex
	layers []domain.Layer
}

// NewCanvas seeds the canvas. Layer ids are stable so a scripted tool call can
// address them.
func NewCanvas(layers ...domain.Layer) *Canvas {
	return &Canvas{layers: append([]domain.Layer(nil), layers...)}
}

func (c *Canvas) ListLayers(ctx context.Context) ([]domain.Layer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]domain.Layer(nil), c.layers...), nil
}

func (c *Canvas) SetWiggle(ctx context.Context, layerID string, jiggle, speed int) (domain.Layer, error) {
	if err := ctx.Err(); err != nil {
		return domain.Layer{}, err
	}
	if err := domain.ValidateWiggle(jiggle, speed); err != nil {
		return domain.Layer{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	layer, err := domain.FindLayer(c.layers, layerID)
	if err != nil {
		return domain.Layer{}, err
	}
	if layer.Locked {
		return domain.Layer{}, fmt.Errorf("canvas: layer %s is locked", layer.ID)
	}

	for i := range c.layers {
		if c.layers[i].ID == layer.ID {
			c.layers[i].Jiggle = jiggle
			c.layers[i].Speed = speed
			return c.layers[i], nil
		}
	}
	return domain.Layer{}, domain.ErrLayerNotFound
}

func (c *Canvas) ToggleLayerVisibility(ctx context.Context, layerID string) (domain.Layer, error) {
	if err := ctx.Err(); err != nil {
		return domain.Layer{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	layer, err := domain.FindLayer(c.layers, layerID)
	if err != nil {
		return domain.Layer{}, err
	}

	for i := range c.layers {
		if c.layers[i].ID == layer.ID {
			c.layers[i].Visible = !c.layers[i].Visible
			return c.layers[i], nil
		}
	}
	return domain.Layer{}, domain.ErrLayerNotFound
}

// ExportProject simulates the export: no encoder, just a description of what
// would be produced with the visible layers of the canvas.
func (c *Canvas) ExportProject(ctx context.Context, spec domain.ExportSpec) (domain.ExportResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ExportResult{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	visible := 0
	for _, layer := range c.layers {
		if layer.Visible {
			visible++
		}
	}

	return domain.ExportResult{Description: fmt.Sprintf(
		"exportaría %s a %dx con %d fps usando %d de %d capas (las ocultas no se exportan)",
		spec.Format, spec.Scale, spec.FPS, visible, len(c.layers),
	)}, nil
}
