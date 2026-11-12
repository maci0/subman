package model

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateRunes(t *testing.T) {
	if got := TruncateRunes("café", 3); got != "caf" {
		t.Fatalf("short cut: %q", got)
	}
	if got := TruncateRunes("café", 4); got != "café" {
		t.Fatalf("exact: %q", got)
	}
	if got := TruncateRunes("café", 0); got != "" {
		t.Fatalf("zero: %q", got)
	}
	long := strings.Repeat("é", MaxNameLen+3)
	got := TruncateRunes(long, MaxNameLen)
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != MaxNameLen {
		t.Fatalf("long: %d runes, valid %v", utf8.RuneCountInString(got), utf8.ValidString(got))
	}
}
