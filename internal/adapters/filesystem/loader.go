// Package filesystem implements the DocumentLoader port over a local directory.
//
// The application must not know that the knowledge base lives on disk, so
// os.ReadFile stays here, inside an adapter.
package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/r0lm0/doodle-assistant/internal/domain"
)

// ErrNoDocuments means the directory had no markdown at all, which is almost
// always a wrong path rather than an empty knowledge base.
var ErrNoDocuments = errors.New("no markdown documents found")

var firstHeadingRE = regexp.MustCompile(`^ {0,3}#[ \t]+(.+?)[ \t]*#*[ \t]*$`)

// Loader reads every *.md file under Dir, recursively, in a stable order.
type Loader struct {
	dir string
}

func NewLoader(dir string) (*Loader, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, errors.New("filesystem loader: directory is required")
	}
	return &Loader{dir: dir}, nil
}

func (l *Loader) Load(ctx context.Context) ([]domain.Document, error) {
	var docs []domain.Document

	err := filepath.WalkDir(l.dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		rel, err := filepath.Rel(l.dir, path)
		if err != nil {
			return fmt.Errorf("relative path of %s: %w", path, err)
		}

		docs = append(docs, domain.Document{
			SourcePath: filepath.ToSlash(rel),
			Title:      titleOf(string(content), entry.Name()),
			Content:    string(content),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load documents from %s: %w", l.dir, err)
	}

	sort.Slice(docs, func(i, j int) bool { return docs[i].SourcePath < docs[j].SourcePath })

	if len(docs) == 0 {
		return nil, fmt.Errorf("load documents from %s: %w", l.dir, ErrNoDocuments)
	}
	return docs, nil
}

// titleOf prefers the document's first H1; a file without one still needs a
// human-readable title in citations.
func titleOf(content, fileName string) string {
	for _, line := range strings.Split(content, "\n") {
		if m := firstHeadingRE.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return strings.TrimSuffix(fileName, filepath.Ext(fileName))
}
