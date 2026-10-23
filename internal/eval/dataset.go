// Package eval measures retrieval quality: what does not get measured does not
// get improved.
//
// It is a development tool, not a domain concept: it reads a dataset, runs the
// retriever and computes recall@K and MRR over the results.
package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Question is one evaluation case: a user question plus the evidence that a
// correct retrieval must surface.
type Question struct {
	ID              string   `json:"id"`
	Question        string   `json:"question"`
	ExpectedSources []string `json:"expectedSources"`
	ExpectedHeading string   `json:"expectedHeading"`
	ExpectedText    string   `json:"expectedText"`
}

type datasetFile struct {
	Name      string     `json:"name"`
	Version   int        `json:"version"`
	Questions []Question `json:"questions"`
}

// LoadQuestions reads and validates the dataset. An invalid dataset is a
// measurement bug, so it fails instead of silently scoring fewer questions.
func LoadQuestions(path string) ([]Question, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read questions %s: %w", path, err)
	}

	var file datasetFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("decode questions %s: %w", path, err)
	}
	if len(file.Questions) == 0 {
		return nil, fmt.Errorf("questions %s: dataset is empty", path)
	}

	seen := make(map[string]bool, len(file.Questions))
	for i, question := range file.Questions {
		if strings.TrimSpace(question.ID) == "" {
			return nil, fmt.Errorf("questions %s: entry %d has no id", path, i)
		}
		if seen[question.ID] {
			return nil, fmt.Errorf("questions %s: duplicate id %q", path, question.ID)
		}
		seen[question.ID] = true
		if strings.TrimSpace(question.Question) == "" {
			return nil, fmt.Errorf("questions %s: entry %q has no question text", path, question.ID)
		}
		if len(question.ExpectedSources) == 0 {
			return nil, fmt.Errorf("questions %s: entry %q has no expected source", path, question.ID)
		}
	}

	return file.Questions, nil
}

// errNoQuestions keeps the error honest when a caller passes an empty slice.
var errNoQuestions = errors.New("eval: no questions to evaluate")
