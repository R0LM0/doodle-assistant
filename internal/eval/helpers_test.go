package eval_test

import (
	"os"
	"path/filepath"
	"testing"
)

func writeDatasetFile(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "questions.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write dataset: %v", err)
	}
	return path
}
