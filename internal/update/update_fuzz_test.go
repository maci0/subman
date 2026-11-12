package update

import (
	"encoding/hex"
	"strings"
	"testing"
)

// FuzzSumFor feeds arbitrary checksum files to the parser that gates a
// binary replace. A hit must be the lower-case hex of exactly 32 bytes,
// and a miss must not be reported as a hit.
func FuzzSumFor(f *testing.F) {
	f.Add("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  subman\n", "subman")
	f.Add("deadbeef  subman\n", "subman")
	f.Add("no space\n", "subman")
	f.Add("", "subman")
	f.Add("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef *subman\n", "subman")
	f.Fuzz(func(t *testing.T, sums, name string) {
		got, ok := sumFor(sums, name)
		if !ok {
			if got != "" {
				t.Fatalf("miss returned %q", got)
			}
			return
		}
		raw, err := hex.DecodeString(got)
		if err != nil || len(raw) != 32 || got != strings.ToLower(got) {
			t.Fatalf("accepted %q", got)
		}
		for _, line := range strings.Split(sums, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == got && fields[1] == name {
				return
			}
		}
		t.Fatalf("accepted %q with no matching line", got)
	})
}
