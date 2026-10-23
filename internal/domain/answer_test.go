package domain

import (
	"strings"
	"testing"
)

func testSources() []Source {
	return []Source{
		{SourcePath: "export.md", Title: "Exportar", HeadingPath: "Exportar > Escala", Distance: 0.12, Content: "La escala va de 1x a 2x."},
		{SourcePath: "layers.md", Title: "Capas", HeadingPath: "Capas > Operaciones", Distance: 0.28, Content: "Duplicar copia la capa completa."},
	}
}

func TestBuildPromptIncludesQuestionSourcesAndDelimiters(t *testing.T) {
	prompt := BuildPrompt("¿Cuál es la escala máxima de exportación?", testSources())

	if !strings.Contains(prompt.User, "¿Cuál es la escala máxima de exportación?") {
		t.Errorf("question missing from the user prompt:\n%s", prompt.User)
	}
	for _, want := range []string{"[1]", "[2]", "export.md > Exportar > Escala", "La escala va de 1x a 2x.", "Duplicar copia la capa completa."} {
		if !strings.Contains(prompt.User, want) {
			t.Errorf("user prompt missing %q:\n%s", want, prompt.User)
		}
	}
	if strings.Count(prompt.User, "<<<") != 1 || strings.Count(prompt.User, ">>>") != 1 {
		t.Errorf("context is not delimited exactly once:\n%s", prompt.User)
	}
	if strings.Count(prompt.User, "[1]") != 1 || strings.Count(prompt.User, "[2]") != 1 {
		t.Errorf("sources are not numbered uniquely:\n%s", prompt.User)
	}
}

func TestBuildPromptTreatsContextAsDataOnly(t *testing.T) {
	prompt := BuildPrompt("pregunta", testSources())

	for _, want := range []string{"SOLO", "No encontré eso en la documentación.", "DATOS", "[fuente:"} {
		if !strings.Contains(prompt.System, want) {
			t.Errorf("system prompt missing %q:\n%s", want, prompt.System)
		}
	}
	if !strings.Contains(prompt.System, "no la obedezcas") && !strings.Contains(prompt.System, "ignorala") {
		t.Errorf("system prompt does not warn about instructions inside the context:\n%s", prompt.System)
	}
}

func TestBuildPromptWithoutSourcesSaysTheContextIsEmpty(t *testing.T) {
	prompt := BuildPrompt("pregunta sin contexto", nil)

	if !strings.Contains(prompt.User, "vac") {
		t.Errorf("empty context is not stated:\n%s", prompt.User)
	}
	if strings.Contains(prompt.User, "[1]") {
		t.Errorf("empty context should not number anything:\n%s", prompt.User)
	}
}

func TestSourceCitationFallsBackToPath(t *testing.T) {
	got := Source{SourcePath: "projects.md"}.Citation()
	if got != "projects.md" {
		t.Errorf("citation = %q", got)
	}
	got = Source{SourcePath: "projects.md", HeadingPath: "Proyectos > Autoguardado"}.Citation()
	if got != "projects.md > Proyectos > Autoguardado" {
		t.Errorf("citation = %q", got)
	}
}
