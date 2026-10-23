package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Document is one source markdown file of the knowledge base.
//
// ContentHash identifies the content of the file, not its location: it is the
// idempotency key of ingestion. SourcePath identifies the document itself.
type Document struct {
	ID          int64
	SourcePath  string
	Title       string
	ContentHash string
	Content     string
	Chunks      []Chunk
}

// Chunk is a retrievable fragment of a Document, with the heading path that
// gives it context and the embedding that makes it searchable.
type Chunk struct {
	ID          int64
	DocumentID  int64
	HeadingPath string
	Position    int
	Content     string
	Embedding   []float32
}

// EmbeddingText is the exact text sent to the embedding model: the heading path
// gives the fragment context ("Exportar > Fondo" says a lot that "solo las capas
// visibles" alone does not), while Content stays clean for citations.
func (c Chunk) EmbeddingText() string {
	if c.HeadingPath == "" {
		return c.Content
	}
	return c.HeadingPath + "\n\n" + c.Content
}

// ContentHash returns the SHA-256 of the normalized content, hex encoded.
func ContentHash(content string) string {
	sum := sha256.Sum256([]byte(NormalizeContent(content)))
	return hex.EncodeToString(sum[:])
}

// NormalizeContent makes semantically equal files hash equal: CRLF becomes LF and
// trailing whitespace per line is dropped. Nothing else is touched, because
// indentation inside code blocks is meaningful.
func NormalizeContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// IngestHash is the idempotency key of ingestion: content plus the chunking
// configuration that produced the chunks.
//
// Hashing the content alone would be a silent trap: changing the chunk size would
// look like "nothing changed" and the index would keep the old chunks forever.
func IngestHash(content string, opts ChunkOptions) string {
	return ContentHash(content + "\n#chunking:" + opts.Signature())
}
