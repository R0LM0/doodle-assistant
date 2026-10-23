package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func writeDataset(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "questions.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write dataset: %v", err)
	}
	return path
}

func TestLoadQuestionsAcceptsAValidDataset(t *testing.T) {
	path := writeDataset(t, `{"version":1,"questions":[
		{"id":"q1","question":"¿cuántas capas?","expectedSources":["layers.md"],"expectedHeading":"capa"},
		{"id":"q2","question":"¿tope de export?","expectedSources":["export.md","export-2.md"]}
	]}`)

	questions, err := LoadQuestions(path)
	if err != nil {
		t.Fatalf("LoadQuestions: %v", err)
	}
	if len(questions) != 2 {
		t.Fatalf("questions = %d, want 2", len(questions))
	}
	if questions[1].ExpectedSources[1] != "export-2.md" {
		t.Errorf("multiple expected sources not parsed: %+v", questions[1])
	}
}

func TestLoadQuestionsRejectsInvalidDatasets(t *testing.T) {
	cases := map[string]string{
		"empty":          `{"questions":[]}`,
		"duplicate id":   `{"questions":[{"id":"q1","question":"a","expectedSources":["a.md"]},{"id":"q1","question":"b","expectedSources":["b.md"]}]}`,
		"missing source": `{"questions":[{"id":"q1","question":"a"}]}`,
		"missing text":   `{"questions":[{"id":"q1","expectedSources":["a.md"]}]}`,
		"missing id":     `{"questions":[{"question":"a","expectedSources":["a.md"]}]}`,
		"not json":       `nope`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadQuestions(writeDataset(t, body)); err == nil {
				t.Fatalf("expected an error for %s", name)
			}
		})
	}

	if _, err := LoadQuestions(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("expected an error for a missing file")
	}
}
