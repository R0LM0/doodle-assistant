package domain

import (
	"strings"
	"testing"
)

func splitOpts() ChunkOptions {
	return ChunkOptions{MaxChars: 120, OverlapChars: 20, MinChars: 0}
}

func TestChunkSplitsByHeadingAndKeepsHierarchy(t *testing.T) {
	md := "# Pinceles\n\n## Pinceles disponibles\n\nContenido A.\n\n## Borrado por trazo\n\nContenido B.\n"

	chunks := ChunkMarkdown(md, splitOpts())

	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2: %+v", len(chunks), chunks)
	}
	if chunks[0].HeadingPath != "Pinceles > Pinceles disponibles" {
		t.Errorf("chunk 0 heading path = %q", chunks[0].HeadingPath)
	}
	if chunks[1].HeadingPath != "Pinceles > Borrado por trazo" {
		t.Errorf("chunk 1 heading path = %q", chunks[1].HeadingPath)
	}
	if chunks[0].Content != "Contenido A." {
		t.Errorf("chunk 0 content = %q", chunks[0].Content)
	}
	if chunks[0].Position != 0 || chunks[1].Position != 1 {
		t.Errorf("positions = %d,%d", chunks[0].Position, chunks[1].Position)
	}
}

func TestChunkIgnoresHeadingsInsideCodeFence(t *testing.T) {
	md := "# Guia\n\n## Ejemplo\n\n```md\n## esto no es un encabezado\n```\n\ntexto final\n"

	chunks := ChunkMarkdown(md, splitOpts())

	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1: %+v", len(chunks), chunks)
	}
	if !strings.Contains(chunks[0].Content, "## esto no es un encabezado") {
		t.Errorf("code fence content lost: %q", chunks[0].Content)
	}
}

func TestChunkSplitsLongSectionWithOverlap(t *testing.T) {
	paragraphs := []string{
		"Parrafo uno con suficiente texto para llenar espacio. relleno uno.",
		"Parrafo dos con suficiente texto para llenar espacio. relleno dos.",
		"Parrafo tres con suficiente texto para llenar espacio. relleno tres.",
		"Parrafo cuatro con suficiente texto para llenar espacio. relleno cuatro.",
	}
	md := "# Guia\n\n## Seccion\n\n" + strings.Join(paragraphs, "\n\n") + "\n"

	chunks := ChunkMarkdown(md, splitOpts())

	if len(chunks) < 3 {
		t.Fatalf("got %d chunks, want >= 3", len(chunks))
	}
	for i, c := range chunks {
		if len(c.Content) > splitOpts().MaxChars {
			t.Errorf("chunk %d is %d chars, over max %d", i, len(c.Content), splitOpts().MaxChars)
		}
	}
	for i := 1; i < len(chunks); i++ {
		if got := overlapPrefix(chunks[i-1].Content, chunks[i].Content); got < splitOpts().OverlapChars/2 {
			t.Errorf("chunk %d overlaps the previous one by only %d chars (want >= %d)\nprev: %q\nnext: %q",
				i, got, splitOpts().OverlapChars/2, chunks[i-1].Content, chunks[i].Content)
		}
	}
}

func TestChunkNeverSplitsInsideCodeFence(t *testing.T) {
	fence := "```go\n" + strings.Repeat("linea de codigo larga\n", 12) + "```"
	md := "# Guia\n\n## Codigo\n\nantes\n\n" + fence + "\n\ndespues\n"

	chunks := ChunkMarkdown(md, splitOpts())

	for i, c := range chunks {
		if strings.Count(c.Content, "```") == 1 {
			t.Fatalf("chunk %d has a broken code fence: %q", i, c.Content)
		}
	}
	joined := ""
	for _, c := range chunks {
		joined += c.Content + "\n"
	}
	if !strings.Contains(joined, fence) {
		t.Fatalf("fence was modified while chunking")
	}
}

func TestChunkMergesTinySections(t *testing.T) {
	opts := ChunkOptions{MaxChars: 200, OverlapChars: 0, MinChars: 40}
	md := "# Guia\n\n## Larga\n\n" + strings.Repeat("texto util. ", 5) + "\n\n## Corta\n\nok\n"

	chunks := ChunkMarkdown(md, opts)

	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1 (tiny section merged): %+v", len(chunks), chunks)
	}
	if !strings.Contains(chunks[0].Content, "ok") {
		t.Errorf("merged chunk lost the tiny section: %q", chunks[0].Content)
	}
}

func TestChunkDropsEmptySections(t *testing.T) {
	md := "# Guia\n\n## Vacia\n\n## Otra\n\ntexto\n"

	chunks := ChunkMarkdown(md, splitOpts())

	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1: %+v", len(chunks), chunks)
	}
	if chunks[0].HeadingPath != "Guia > Otra" {
		t.Errorf("heading path = %q", chunks[0].HeadingPath)
	}
}

func TestChunkWithoutHeadingsReturnsSingleChunk(t *testing.T) {
	chunks := ChunkMarkdown("solo un parrafo sin encabezados\n", splitOpts())

	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	if chunks[0].HeadingPath != "" {
		t.Errorf("heading path = %q, want empty", chunks[0].HeadingPath)
	}
	if chunks[0].Content != "solo un parrafo sin encabezados" {
		t.Errorf("content = %q", chunks[0].Content)
	}
}

func TestChunkNormalizesCRLFAndClosesHeadingStack(t *testing.T) {
	crlf := "# A\r\n\r\n## B\r\n\r\ntexto\r\n\r\n# C\r\n\r\notro\r\n"
	lf := strings.ReplaceAll(crlf, "\r\n", "\n")

	gotCRLF := ChunkMarkdown(crlf, splitOpts())
	gotLF := ChunkMarkdown(lf, splitOpts())

	if len(gotCRLF) != len(gotLF) || len(gotLF) != 2 {
		t.Fatalf("chunk counts differ: %d vs %d", len(gotCRLF), len(gotLF))
	}
	if gotCRLF[1].HeadingPath != "C" {
		t.Errorf("sibling H1 did not close the stack: %q", gotCRLF[1].HeadingPath)
	}
	for i := range gotLF {
		if gotCRLF[i].HeadingPath != gotLF[i].HeadingPath ||
			gotCRLF[i].Content != gotLF[i].Content ||
			gotCRLF[i].Position != gotLF[i].Position {
			t.Errorf("chunk %d differs between CRLF and LF: %+v vs %+v", i, gotCRLF[i], gotLF[i])
		}
	}
}

// overlapPrefix returns how many leading bytes of next are also the trailing
// bytes of prev, i.e. the size of the real overlap between two chunks.
func overlapPrefix(prev, next string) int {
	limit := len(next)
	if len(prev) < limit {
		limit = len(prev)
	}
	for k := limit; k > 0; k-- {
		if strings.HasSuffix(prev, next[:k]) {
			return k
		}
	}
	return 0
}
