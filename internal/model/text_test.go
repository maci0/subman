package model

import (
	"math"
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

// A cut that lands exactly on a rune boundary must keep that rune and must
// not read past the end of the buffer.
func TestTruncateBytesOnARuneBoundary(t *testing.T) {
	body := []byte(strings.Repeat("a", 4) + "é")
	got := TruncateBytes(body, 4)
	if string(got) != "aaaa" {
		t.Fatalf("boundary: %q", got)
	}
	split := TruncateBytes(body, 5)
	if string(split) != "aaaa" || !utf8.Valid(split) {
		t.Fatalf("inside a rune: %q", split)
	}
	if got := TruncateBytes(body, 0); got != nil {
		t.Fatalf("zero: %q", got)
	}
	if got := TruncateBytes(body, len(body)); string(got) != string(body) {
		t.Fatalf("whole: %q", got)
	}
}

func TestPriceValid(t *testing.T) {
	for _, p := range []Price{{0, "USD"}, {10.98, "SGD"}} {
		if !p.Valid() {
			t.Errorf("%+v refused", p)
		}
	}
	for _, p := range []Price{{-1, "USD"}, {math.NaN(), "USD"}, {math.Inf(1), "USD"}, {1, "usd"}, {1, "US"}, {1, "../"}} {
		if p.Valid() {
			t.Errorf("%+v accepted", p)
		}
	}
}
