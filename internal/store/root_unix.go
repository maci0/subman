//go:build !windows

package store

import (
	"os"
	"path/filepath"
)

// defaultDataBase is the freedesktop per-user data directory. macOS releases
// honour $XDG_DATA_HOME the same way, and fall back here when it is unset.
func defaultDataBase() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}
