// Package archtest enforces the hexagonal dependency rule with the Go parser
// instead of a convention in a document nobody reads.
package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/r0lm0/doodle-assistant/"

// TestDomainAndAppDoNotImportAdapters is the rule from the project brief: the
// inner layers never depend on the outer ones.
func TestDomainAndAppDoNotImportAdapters(t *testing.T) {
	forbidden := []string{modulePath + "internal/adapters"}

	assertNoImports(t, "../domain", forbidden)
	assertNoImports(t, "../app", forbidden)
}

// TestDomainHasNoProjectDependencies keeps domain pure: stdlib only.
func TestDomainHasNoProjectDependencies(t *testing.T) {
	assertNoImports(t, "../domain", []string{modulePath + "internal/"})
}

// TestPortsDoNotImportAdapters keeps the ports layer honest too.
func TestPortsDoNotImportAdapters(t *testing.T) {
	assertNoImports(t, "../ports", []string{modulePath + "internal/adapters"})
}

func assertNoImports(t *testing.T, dir string, forbidden []string) {
	t.Helper()

	fset := token.NewFileSet()
	checked := 0

	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		checked++

		for _, spec := range file.Imports {
			imported := strings.Trim(spec.Path.Value, `"`)
			for _, prefix := range forbidden {
				if strings.HasPrefix(imported, prefix) {
					t.Errorf("%s:%d imports %q, which is forbidden in this layer",
						path, fset.Position(spec.Pos()).Line, imported)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	if checked == 0 {
		t.Fatalf("no Go files checked under %s; the test would pass vacuously", dir)
	}
}
