package eval

import (
	"fmt"
	"sort"
	"strings"

	"github.com/r0lm0/doodle-assistant/internal/ports"
)

// Retrieved is what the retriever returned for one question.
type Retrieved struct {
	Question Question
	Hits     []ports.SearchHit
}

// IsRelevant decides whether a hit counts as evidence for a question.
//
// The source document must always match. Then, when the dataset names an expected
// heading, the heading must contain it *or* the chunk must contain the expected
// text. That fallback matters: merging small sections into the previous chunk is a
// legitimate chunking choice that preserves the evidence while changing its
// heading, and a metric that punishes it measures the chunker, not retrieval.
func IsRelevant(question Question, hit ports.SearchHit) bool {
	documentOK := false
	for _, source := range question.ExpectedSources {
		if strings.EqualFold(strings.TrimSpace(source), hit.SourcePath) {
			documentOK = true
			break
		}
	}
	if !documentOK {
		return false
	}

	expectedHeading := strings.ToLower(strings.TrimSpace(question.ExpectedHeading))
	if expectedHeading == "" {
		return true
	}
	if strings.Contains(strings.ToLower(hit.HeadingPath), expectedHeading) {
		return true
	}

	expectedText := strings.ToLower(strings.TrimSpace(question.ExpectedText))
	return expectedText != "" && strings.Contains(strings.ToLower(hit.Content), expectedText)
}

// RecallAtK is the fraction of questions whose evidence appears in the top K.
// It counts questions, not hits: finding the same source three times does not
// make a question better answered.
func RecallAtK(results []Retrieved, k int) float64 {
	if len(results) == 0 || k <= 0 {
		return 0
	}

	found := 0
	for _, result := range results {
		limit := k
		if limit > len(result.Hits) {
			limit = len(result.Hits)
		}
		for _, hit := range result.Hits[:limit] {
			if IsRelevant(result.Question, hit) {
				found++
				break
			}
		}
	}
	return float64(found) / float64(len(results))
}

// MRR is the mean reciprocal rank of the first relevant hit: 1.0 when the correct
// chunk is always first, 0 when it never shows up.
func MRR(results []Retrieved) float64 {
	if len(results) == 0 {
		return 0
	}

	total := 0.0
	for _, result := range results {
		for rank, hit := range result.Hits {
			if IsRelevant(result.Question, hit) {
				total += 1 / float64(rank+1)
				break
			}
		}
	}
	return total / float64(len(results))
}

// Report is the measurable outcome of one evaluation run.
type Report struct {
	Questions int
	TopK      int
	Recall    map[int]float64
	MRR       float64
	Misses    []string

	// Ranks maps a question id to the rank (1-based) of its first relevant hit;
	// 0 means no relevant hit at all. Verbose adds them to the table: knowing
	// which question fails is the difference between a number and a lead.
	Ranks   map[string]int
	Verbose bool
}

// String renders a compact table, ordered by K, so two runs can be compared by
// eye without post-processing.
func (r Report) String() string {
	var b strings.Builder

	fmt.Fprintf(&b, "questions=%d top_k=%d\n", r.Questions, r.TopK)
	fmt.Fprintf(&b, "%-10s %s\n", "metric", "value")
	for _, k := range sortedKs(r.Recall) {
		fmt.Fprintf(&b, "%-10s %.3f\n", fmt.Sprintf("recall@%d", k), r.Recall[k])
	}
	fmt.Fprintf(&b, "%-10s %.3f\n", "MRR", r.MRR)

	if len(r.Misses) > 0 {
		fmt.Fprintf(&b, "misses: %s\n", strings.Join(r.Misses, ", "))
	}

	if r.Verbose && len(r.Ranks) > 0 {
		b.WriteString("per question:\n")
		ids := make([]string, 0, len(r.Ranks))
		for id := range r.Ranks {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			rank := r.Ranks[id]
			label := fmt.Sprintf("rank %d", rank)
			if rank == 0 {
				label = "not found"
			}
			fmt.Fprintf(&b, "  %-5s %s\n", id, label)
		}
	}

	return b.String()
}

func sortedKs(recall map[int]float64) []int {
	ks := make([]int, 0, len(recall))
	for k := range recall {
		ks = append(ks, k)
	}
	for i := 1; i < len(ks); i++ {
		for j := i; j > 0 && ks[j] < ks[j-1]; j-- {
			ks[j], ks[j-1] = ks[j-1], ks[j]
		}
	}
	return ks
}
