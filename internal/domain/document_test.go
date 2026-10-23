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
