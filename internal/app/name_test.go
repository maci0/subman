package app

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/maci0/subman/internal/model"
)

// A login email past MaxNameLen is shortened to whole characters. "é" is two
// bytes, so cutting the same limit in bytes would store invalid UTF-8.
func TestNewRequestShortensOnARuneBoundary(t *testing.T) {
	email := strings.Repeat("é", model.MaxNameLen) + "@example.com"
	got := NewRequest(model.ImportRequest{Provider: model.ProviderClaude}, email)
	if !utf8.ValidString(got.Name) {
		t.Fatalf("name is not valid UTF-8: %q", got.Name)
	}
	if n := utf8.RuneCountInString(got.Name); n != model.MaxNameLen {
		t.Fatalf("name is %d runes, want %d", n, model.MaxNameLen)
	}
}
