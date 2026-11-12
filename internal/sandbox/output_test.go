package sandbox

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// outputString caps a byte budget without ending inside a multi-byte
// character. The last byte of a maxOutput cut lands on the second byte of
// "é", which would otherwise be stored as invalid UTF-8.
func TestOutputStringEndsOnARune(t *testing.T) {
	body := []byte(strings.Repeat("a", maxOutput-1) + "é" + "tail")
	got := outputString(body)
	text, ok := strings.CutSuffix(got, "\n…truncated")
	if !ok {
		t.Fatal("missing truncation marker")
	}
	if !utf8.ValidString(text) {
		t.Fatalf("cut inside a rune: %q", text[len(text)-4:])
	}
	if !strings.HasSuffix(text, "a") {
		t.Fatalf("kept the split character: %q", text[len(text)-4:])
	}
}
