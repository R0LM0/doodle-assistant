package eval

import (
	"strings"
	"testing"

	"github.com/r0lm0/doodle-assistant/internal/ports"
)

func hit(source, heading string) ports.SearchHit {
	return ports.SearchHit{SourcePath: source, HeadingPath: heading, Content: "contenido"}
}

func TestIsRelevantRequiresTheExpectedDocumentAndHeading(t *testing.T) {
	q := Question{ID: "q1", ExpectedSources: []string{"export.md"}, ExpectedHeading: "Escala"}

	if !IsRelevant(q, hit("export.md", "Exportar > Escala")) {
		t.Error("matching document and heading must be relevant")
	}
	if IsRelevant(q, hit("export.md", "Exportar > Fondo")) {
		t.Error("right document but wrong heading is not relevant")
	}
	if IsRelevant(q, hit("images.md", "Imágenes > Escala")) {
		t.Error("right heading in the wrong document is not relevant")
	}
}

func TestIsRelevantAcceptsMergedChunksByTheirContent(t *testing.T) {
	q := Question{ID: "q1", ExpectedSources: []string{"history.md"}, ExpectedHeading: "Límite", ExpectedText: "200 pasos"}

	merged := ports.SearchHit{SourcePath: "history.md", HeadingPath: "Deshacer y rehacer > Qué cuenta como un paso", Content: "El historial guarda los últimos 200 pasos."}
	if !IsRelevant(q, merged) {
		t.Error("a merged chunk that still contains the evidence must be relevant")
	}

	wrongDoc := merged
	wrongDoc.SourcePath = "export.md"
	if IsRelevant(q, wrongDoc) {
		t.Error("the expected text in the wrong document is not relevant")
	}

	noEvidence := ports.SearchHit{SourcePath: "history.md", HeadingPath: "Deshacer y rehacer > Atajos", Content: "Ctrl + Z"}
	if IsRelevant(q, noEvidence) {
		t.Error("neither heading nor content match must not be relevant")
	}
}

func TestIsRelevantWithoutExpectedHeadingAcceptsAnyHeading(t *testing.T) {
	q := Question{ID: "q1", ExpectedSources: []string{"export.md"}}

	if !IsRelevant(q, hit("export.md", "Exportar > Fondo")) {
		t.Error("without a heading constraint any heading of the document counts")
	}
}

func TestRecallAtKCountsQuestionsNotHits(t *testing.T) {
	results := []Retrieved{
		{Question: Question{ID: "q1", ExpectedSources: []string{"a.md"}}, Hits: []ports.SearchHit{
			hit("b.md", "x"), hit("a.md", "x"),
		}},
		{Question: Question{ID: "q2", ExpectedSources: []string{"c.md"}}, Hits: []ports.SearchHit{
			hit("d.md", "x"), hit("e.md", "x"),
		}},
	}

	if got := RecallAtK(results, 1); got != 0 {
		t.Errorf("recall@1 = %v, want 0 (no question has the source first)", got)
	}
	if got := RecallAtK(results, 2); got != 0.5 {
		t.Errorf("recall@2 = %v, want 0.5", got)
	}
	if got := RecallAtK(results, 5); got != 0.5 {
		t.Errorf("recall@5 = %v, want 0.5 (q2 never matches)", got)
	}
	if got := RecallAtK(nil, 3); got != 0 {
		t.Errorf("recall of an empty result set = %v, want 0", got)
	}
}

func TestRecallAtKTakesTheBestRankPerQuestion(t *testing.T) {
	results := []Retrieved{
		{Question: Question{ID: "q1", ExpectedSources: []string{"a.md"}}, Hits: []ports.SearchHit{
			hit("x.md", ""), hit("y.md", ""), hit("a.md", ""),
		}},
	}

	if got := RecallAtK(results, 2); got != 0 {
		t.Errorf("recall@2 = %v, want 0 (the hit is at rank 3)", got)
	}
	if got := RecallAtK(results, 3); got != 1 {
		t.Errorf("recall@3 = %v, want 1", got)
	}
}

func TestMRRRewardsEarlyRanks(t *testing.T) {
	early := []Retrieved{
		{Question: Question{ID: "q1", ExpectedSources: []string{"a.md"}}, Hits: []ports.SearchHit{hit("a.md", "")}},
	}
	late := []Retrieved{
		{Question: Question{ID: "q1", ExpectedSources: []string{"a.md"}}, Hits: []ports.SearchHit{
			hit("x.md", ""), hit("a.md", ""),
		}},
	}

	if got := MRR(early); got != 1 {
		t.Errorf("MRR with a first-rank hit = %v, want 1", got)
	}
	if got := MRR(late); got != 0.5 {
		t.Errorf("MRR with a second-rank hit = %v, want 0.5", got)
	}
}

func TestReportRendersTheRecallTable(t *testing.T) {
	report := Report{
		Questions: 2,
		TopK:      4,
		Recall:    map[int]float64{1: 0.5, 4: 1},
		MRR:       0.75,
	}

	text := report.String()
	for _, want := range []string{"recall@1", "0.500", "recall@4", "1.000", "MRR", "0.750", "questions=2"} {
		if !strings.Contains(text, want) {
			t.Errorf("report missing %q:\n%s", want, text)
		}
	}
}
