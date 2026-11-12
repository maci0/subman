package quota

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/maci0/subman/internal/model"
)

// A label longer than MaxNameLen is cut on a rune boundary. "é" is two bytes,
// so a byte cut of the same length would land inside it and store an invalid
// UTF-8 label.
func TestTrimLabelKeepsWholeRunes(t *testing.T) {
	name := strings.Repeat("é", model.MaxNameLen+1)
	body := `{"plan_type":"` + name + `","rate_limit":{"primary_window":{"used_percent":1,"limit_window_seconds":18000}}}`
	r := Parse(model.ProviderCodex, []byte(body), time.Now())
	if !r.OK {
		t.Fatalf("not ok: %+v", r)
	}
	if !utf8.ValidString(r.Plan) {
		t.Fatalf("plan is not valid UTF-8: %q", r.Plan)
	}
	if got := utf8.RuneCountInString(r.Plan); got != model.MaxNameLen {
		t.Fatalf("plan is %d runes, want %d: %q", got, model.MaxNameLen, r.Plan)
	}
	if r.Plan != "É"+strings.Repeat("é", model.MaxNameLen-1) {
		t.Fatalf("plan %q", r.Plan)
	}
}
