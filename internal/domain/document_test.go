package domain

import "testing"

func TestContentHashIsStableAcrossLineEndingsAndTrailingSpaces(t *testing.T) {
	a := ContentHash("linea uno\r\nlinea dos  \r\n")
	b := ContentHash("linea uno\nlinea dos\n")

	if a != b {
		t.Fatalf("hash differs for equivalent content:\n%s\n%s", a, b)
	}
	if len(a) != 64 {
		t.Errorf("hash length = %d, want 64 hex chars", len(a))
	}
}

func TestContentHashChangesWithContent(t *testing.T) {
	if ContentHash("hola") == ContentHash("hola mundo") {
		t.Error("hash should change when meaningful content changes")
	}
	if ContentHash("hola") == ContentHash("chau") {
		t.Error("hash should differ for different content")
	}
}

func TestContentHashOfEmptyIsDefined(t *testing.T) {
	if ContentHash("") == "" || ContentHash("   \n\n ") == "" {
		t.Error("empty content must still produce a hash")
	}
}

func TestIngestHashIncludesChunkingOptions(t *testing.T) {
	content := "# A\n\ntexto de prueba\n"
	base := ChunkOptions{MaxChars: 1200, OverlapChars: 150, MinChars: 200}

	if IngestHash(content, base) != IngestHash(content, base) {
		t.Error("the ingest hash must be stable for the same content and options")
	}

	smaller := base
	smaller.MaxChars = 600
	if IngestHash(content, base) == IngestHash(content, smaller) {
		t.Error("changing the chunk size must change the ingest hash, otherwise re-chunking is skipped forever")
	}

	moreOverlap := base
	moreOverlap.OverlapChars = 300
	if IngestHash(content, base) == IngestHash(content, moreOverlap) {
		t.Error("changing the overlap must change the ingest hash")
	}

	if IngestHash(content, base) == IngestHash("# A\n\notro texto\n", base) {
		t.Error("different content must produce a different ingest hash")
	}
}

func TestChunkOptionsSignatureIsStableAndOrdered(t *testing.T) {
	opts := ChunkOptions{MaxChars: 1200, OverlapChars: 150, MinChars: 200}

	if opts.Signature() == "" {
		t.Fatal("signature must not be empty")
	}
	if opts.Signature() != (ChunkOptions{MaxChars: 1200, OverlapChars: 150, MinChars: 200}).Signature() {
		t.Error("signature must be stable for equal options")
	}
	if opts.Signature() == (ChunkOptions{MaxChars: 600, OverlapChars: 150, MinChars: 200}).Signature() {
		t.Error("signature must differ when a value differs")
	}
}
