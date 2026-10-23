package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// ChunkOptions controls how a markdown document is cut into chunks.
//
// MaxChars is a character budget, not a token budget: the embedding model counts
// tokens, but a character budget needs no tokenizer and is stable across models.
// The mapping is roughly 4 characters per token, so 1200 chars ~= 300 tokens,
// well inside embeddinggemma's 2048-token window.
type ChunkOptions struct {
	MaxChars     int
	OverlapChars int
	MinChars     int
}

// DefaultChunkOptions is the starting point for phase 1. Phase 3 tunes these
// numbers against the evaluation dataset instead of guessing.
func DefaultChunkOptions() ChunkOptions {
	return ChunkOptions{MaxChars: 1200, OverlapChars: 150, MinChars: 200}
}

var headingRE = regexp.MustCompile(`^ {0,3}(#{1,6})[ \t]+(.+?)[ \t]*#*[ \t]*$`)

type section struct {
	headingPath string
	lines       []string
}

type headingLevel struct {
	level int
	text  string
}

// Chunk cuts a markdown document into chunks at heading boundaries, splitting any
// section that exceeds MaxChars and keeping OverlapChars of trailing text between
// consecutive pieces of the same section.
func ChunkMarkdown(markdown string, opts ChunkOptions) []Chunk {
	if opts.MaxChars <= 0 {
		opts = DefaultChunkOptions()
	}

	var chunks []Chunk
	for _, s := range splitSections(NormalizeContent(markdown)) {
		body := strings.TrimSpace(strings.Join(s.lines, "\n"))
		if body == "" {
			continue
		}

		// A section too small to stand alone is folded into the previous chunk
		// when it fits: a chunk with two words retrieves badly and wastes an
		// embedding call.
		if opts.MinChars > 0 && len(body) < opts.MinChars && len(chunks) > 0 {
			last := &chunks[len(chunks)-1]
			merged := last.Content + "\n\n" + body
			if len(merged) <= opts.MaxChars {
				last.Content = merged
				continue
			}
		}

		for _, piece := range splitBody(body, opts) {
			chunks = append(chunks, Chunk{
				HeadingPath: s.headingPath,
				Position:    len(chunks),
				Content:     piece,
			})
		}
	}
	return chunks
}

// splitSections walks the document line by line, tracking the heading stack so
// every section knows its full path, and toggling fence state so a "#" inside a
// code block is not mistaken for a heading.
func splitSections(markdown string) []section {
	var (
		sections []section
		stack    []headingLevel
		current  section
		inFence  bool
	)

	flush := func() {
		sections = append(sections, current)
		current = section{}
	}

	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			current.lines = append(current.lines, line)
			continue
		}

		if !inFence {
			if m := headingRE.FindStringSubmatch(line); m != nil {
				flush()
				level := len(m[1])
				for len(stack) > 0 && stack[len(stack)-1].level >= level {
					stack = stack[:len(stack)-1]
				}
				stack = append(stack, headingLevel{level: level, text: strings.TrimSpace(m[2])})
				current.headingPath = joinHeadingPath(stack)
				continue
			}
		}

		current.lines = append(current.lines, line)
	}
	flush()

	return sections
}

func joinHeadingPath(stack []headingLevel) string {
	parts := make([]string, len(stack))
	for i, h := range stack {
		parts[i] = h.text
	}
	return strings.Join(parts, " > ")
}

// splitBody packs atomic units (paragraphs, whole code fences) into windows of at
// most opts.MaxChars, seeding each new window with the tail of the previous one.
func splitBody(body string, opts ChunkOptions) []string {
	units := expandUnits(splitUnits(body), opts.MaxChars)

	var (
		out    []string
		parts  []string
		curLen int
	)

	for _, unit := range units {
		add := len(unit)
		if curLen > 0 {
			add += 2 // the "\n\n" joiner
		}

		if curLen > 0 && curLen+add > opts.MaxChars {
			out = append(out, strings.Join(parts, "\n\n"))
			seed := overlapSeed(out[len(out)-1], opts.OverlapChars)
			parts, curLen = nil, 0
			// The seed is best effort: if it would not leave room for the unit,
			// the unit wins and this boundary simply has no overlap.
			if seed != "" && len(seed)+2+len(unit) <= opts.MaxChars {
				parts = []string{seed}
				curLen = len(seed)
			}
			add = len(unit)
			if curLen > 0 {
				add += 2
			}
		}

		parts = append(parts, unit)
		curLen += add
	}

	if len(parts) > 0 {
		out = append(out, strings.Join(parts, "\n\n"))
	}
	return out
}

// splitUnits groups lines into paragraphs, keeping fenced code blocks whole so a
// fence is never torn apart.
func splitUnits(body string) []string {
	var (
		units   []string
		buf     []string
		inFence bool
	)

	flush := func() {
		if len(buf) > 0 {
			units = append(units, strings.Join(buf, "\n"))
			buf = nil
		}
	}

	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		isFence := strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")

		switch {
		case inFence:
			buf = append(buf, line)
			if isFence {
				flush()
				inFence = false
			}
		case isFence:
			flush()
			buf = append(buf, line)
			inFence = true
		case trimmed == "":
			flush()
		default:
			buf = append(buf, line)
		}
	}
	flush()

	return units
}

// expandUnits breaks units that are too big to ever fit, except fences: a fence
// longer than MaxChars becomes its own oversized chunk rather than broken code.
func expandUnits(units []string, max int) []string {
	var out []string
	for _, unit := range units {
		if len(unit) <= max || isFence(unit) {
			out = append(out, unit)
			continue
		}
		out = append(out, splitOversized(unit, max)...)
	}
	return out
}

func isFence(unit string) bool {
	trimmed := strings.TrimSpace(unit)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

func splitOversized(unit string, max int) []string {
	var (
		out []string
		buf []string
	)

	flush := func() {
		if len(buf) > 0 {
			out = append(out, strings.Join(buf, "\n"))
			buf = nil
		}
	}

	for _, line := range strings.Split(unit, "\n") {
		if len(line) > max {
			flush()
			out = append(out, hardSplit(line, max)...)
			continue
		}
		candidate := line
		if len(buf) > 0 {
			candidate = strings.Join(buf, "\n") + "\n" + line
		}
		if len(candidate) > max {
			flush()
		}
		buf = append(buf, line)
	}
	flush()

	return out
}

// hardSplit cuts a single line that cannot be broken at any structural boundary,
// preferring the last space inside the window over cutting a word in half.
func hardSplit(line string, max int) []string {
	var out []string
	line = strings.TrimSpace(line)
	for len(line) > max {
		cut := max
		if i := strings.LastIndexByte(line[:max], ' '); i > max/2 {
			cut = i
		}
		out = append(out, strings.TrimSpace(line[:cut]))
		line = strings.TrimSpace(line[cut:])
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

// overlapSeed returns the trailing text of the previous chunk, at most overlap
// bytes, snapped forward to a word boundary so the next chunk does not start in
// the middle of a word.
func overlapSeed(previous string, overlap int) string {
	if overlap <= 0 || len(previous) <= overlap {
		return ""
	}
	start := len(previous) - overlap
	if i := strings.IndexByte(previous[start:], ' '); i >= 0 {
		start += i + 1
	}
	seed := strings.TrimSpace(previous[start:])

	// Never seed with a code fence marker: carrying a closing ``` into the next
	// chunk would leave it with an unbalanced fence.
	if strings.Contains(seed, "```") || strings.Contains(seed, "~~~") {
		return ""
	}
	return seed
}

// Signature is a stable, human-readable identity of the chunking configuration.
// It feeds IngestHash and is safe to log: it contains no document content.
func (o ChunkOptions) Signature() string {
	return fmt.Sprintf("max=%d,overlap=%d,min=%d", o.MaxChars, o.OverlapChars, o.MinChars)
}
