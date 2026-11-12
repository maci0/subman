package version

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// releaseNumber is a quoted x.y.z. The CLI spec's own "0.3" is not one.
var releaseNumber = regexp.MustCompile(`"\d+\.\d+\.\d+"`)

// The release number is declared once, in Release, and a release build
// overrides the binary's own main.release with -X, which main copies here. A
// second literal would survive that stamp: v0.1.1 shipped that way, with the
// CLI and the window still compiled to 0.1.1 after the tag moved on.
func TestReleaseIsDeclaredOnce(t *testing.T) {
	root := repoRoot(t)
	var hits []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		if strings.HasSuffix(path, "_test.go") || path == file(t) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(body), "\n") {
			if releaseNumber.MatchString(line) {
				hits = append(hits, rel(root, path)+":"+itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) > 0 {
		t.Fatalf("a release number is declared outside internal/version:\n%s", strings.Join(hits, "\n"))
	}
	if Release == "" {
		t.Fatal("Release is empty")
	}
}

// file is this test's source, so the walk can skip the declaration it guards.
func file(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "internal", "version", "version.go")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the working directory")
		}
		dir = parent
	}
}

func rel(root, path string) string {
	out, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
