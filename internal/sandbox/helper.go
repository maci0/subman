package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// helperName is the launcher go test builds from cmd/sandbox-helper.
const helperName = "subman-sandbox"

var (
	helperOnce sync.Once
	helperPath string
	helperErr  error
)

// helper is the program Run starts to confine itself and exec the child.
func helper() (string, error) {
	helperOnce.Do(func() {
		helperPath, helperErr = ensureHelper()
	})
	return helperPath, helperErr
}

// ensureHelper returns the running executable: every subman main calls
// MaybeReexec first, so a subman binary is its own launcher and needs
// nothing beside it. A test binary is not subman and would run its tests
// again, so under go test the launcher is built from cmd/sandbox-helper.
func ensureHelper() (string, error) {
	if !testing.Testing() {
		return os.Executable()
	}
	src := helperSource()
	if src == "" {
		return "", fmt.Errorf("the sandbox launcher source (cmd/sandbox-helper) was not found from the test's directory")
	}
	dir := helperDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, helperName)
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", dest, src)
	cmd.Env = os.Environ()
	if msg, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build sandbox helper: %w: %s", err, msg)
	}
	return dest, nil
}

func helperSource() string {
	if root := moduleRoot(); root != "" {
		src := filepath.Join(root, "cmd", "sandbox-helper")
		if st, err := os.Stat(src); err == nil && st.IsDir() {
			return src
		}
	}
	return ""
}

func helperDir() string {
	if self, err := os.Executable(); err == nil {
		dir := filepath.Dir(self)
		if f, err := os.CreateTemp(dir, ".subman-write-*"); err == nil {
			name := f.Name()
			f.Close()
			os.Remove(name)
			return dir
		}
	}
	if data := os.Getenv("SUBMAN_DATA"); data != "" {
		return data
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(cache, "subman")
}

func moduleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "cmd", "sandbox-helper")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
