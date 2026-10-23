package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNewExportSpecValidatesEveryField(t *testing.T) {
	spec, err := NewExportSpec(" WEBM ", 2, 24)
	if err != nil {
		t.Fatalf("NewExportSpec: %v", err)
	}
	if spec.Format != "webm" || spec.Scale != 2 || spec.FPS != 24 {
		t.Errorf("spec = %+v (format must be normalized)", spec)
	}

	cases := []struct {
		format     string
		scale, fps int
	}{
		{"apng", 1, 12},
		{"png", 3, 12},
		{"gif", 1, 0},
		{"gif", 1, 61},
	}
	for _, c := range cases {
		if _, err := NewExportSpec(c.format, c.scale, c.fps); err == nil {
			t.Errorf("expected an error for %s/%d/%d", c.format, c.scale, c.fps)
		}
	}
}

func TestValidateWiggleBounds(t *testing.T) {
	if err := ValidateWiggle(0, 1); err != nil {
		t.Errorf("lowest valid values rejected: %v", err)
	}
	if err := ValidateWiggle(20, 24); err != nil {
		t.Errorf("highest valid values rejected: %v", err)
	}
	if err := ValidateWiggle(-1, 12); err == nil {
		t.Error("expected an error for jiggle -1")
	}
	if err := ValidateWiggle(21, 12); err == nil {
		t.Error("expected an error for jiggle 21")
	}
	if err := ValidateWiggle(5, 0); err == nil {
		t.Error("expected an error for speed 0")
	}
	if err := ValidateWiggle(5, 25); err == nil {
		t.Error("expected an error for speed 25")
	}
}

func TestFindLayer(t *testing.T) {
	layers := []Layer{{ID: "fondo", Name: "Fondo"}, {ID: "crayon", Name: "Crayon"}}

	layer, err := FindLayer(layers, "crayon")
	if err != nil || layer.Name != "Crayon" {
		t.Fatalf("layer = %+v err = %v", layer, err)
	}

	if _, err := FindLayer(layers, "nope"); !errors.Is(err, ErrLayerNotFound) {
		t.Errorf("err = %v, want ErrLayerNotFound", err)
	}
	if _, err := FindLayer(layers, "  "); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("empty id error = %v", err)
	}
}
