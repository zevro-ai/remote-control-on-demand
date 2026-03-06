package codexbot

import (
	"strings"
	"testing"
)

func TestSplitChunksPreservesOriginalText(t *testing.T) {
	text := "Naglowek\n```go\nfunc main() {\n    println(\"żółw\")\n}\n```\n"

	chunks := splitChunks(text, 18)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	if got := strings.Join(chunks, ""); got != text {
		t.Fatalf("splitChunks() changed content\n got: %q\nwant: %q", got, text)
	}
}

func TestSplitChunksPreservesUTF8Runes(t *testing.T) {
	text := strings.Repeat("zażółć gęślą jaźń ", 40)

	chunks := splitChunks(text, 23)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	if got := strings.Join(chunks, ""); got != text {
		t.Fatalf("splitChunks() changed UTF-8 content\n got: %q\nwant: %q", got, text)
	}
}
