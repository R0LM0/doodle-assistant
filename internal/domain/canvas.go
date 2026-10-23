package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Canvas rules. They bound what a tool call may do, independently of who asked:
// a model inventing jiggle=900 must be rejected here, not silently clamped.
const (
	MinJiggle = 0
	MaxJiggle = 20
	MinSpeed  = 1
	MaxSpeed  = 24

	MinFPS = 1
	MaxFPS = 60
)

// Layer is one layer of the drawing canvas.
type Layer struct {
	ID      string
	Name    string
	Visible bool
	Locked  bool
	Jiggle  int
	Speed   int
}

// ExportSpec is a validated export request.
type ExportSpec struct {
	Format string
	Scale  int
	FPS    int
}

// ExportResult describes what an export would produce. Phase 4 simulates the
// export: the interesting part is the agent loop, not a real encoder.
type ExportResult struct {
	Description string
}

// NewExportSpec validates a request coming from a tool call.
func NewExportSpec(format string, scale, fps int) (ExportSpec, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	switch format {
	case "png", "gif", "webm":
	default:
		return ExportSpec{}, fmt.Errorf("canvas: unsupported format %q (png, gif or webm)", format)
	}
	if scale != 1 && scale != 2 {
		return ExportSpec{}, fmt.Errorf("canvas: scale must be 1 or 2, got %d", scale)
	}
	if fps < MinFPS || fps > MaxFPS {
		return ExportSpec{}, fmt.Errorf("canvas: fps must be between %d and %d, got %d", MinFPS, MaxFPS, fps)
	}
	return ExportSpec{Format: format, Scale: scale, FPS: fps}, nil
}

// ValidateWiggle checks the live-animation parameters of one layer.
func ValidateWiggle(jiggle, speed int) error {
	if jiggle < MinJiggle || jiggle > MaxJiggle {
		return fmt.Errorf("canvas: jiggle must be between %d and %d, got %d", MinJiggle, MaxJiggle, jiggle)
	}
	if speed < MinSpeed || speed > MaxSpeed {
		return fmt.Errorf("canvas: speed must be between %d and %d, got %d", MinSpeed, MaxSpeed, speed)
	}
	return nil
}

// ErrLayerNotFound keeps callers from matching on strings.
var ErrLayerNotFound = errors.New("canvas: layer not found")

// FindLayer returns a layer by id.
func FindLayer(layers []Layer, id string) (Layer, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Layer{}, errors.New("canvas: layer id is required")
	}
	for _, layer := range layers {
		if layer.ID == id {
			return layer, nil
		}
	}
	return Layer{}, fmt.Errorf("%w: %s", ErrLayerNotFound, id)
}
