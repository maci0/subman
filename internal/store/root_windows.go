//go:build windows

package store

import (
	"os"
	"path/filepath"
)

// defaultDataBase is the per-user roaming data directory. A Windows release
// keeps its accounts there rather than under the freedesktop ~/.local/share
// layout, which nothing on Windows looks in. APPDATA is already absolute.
func defaultDataBase() (string, error) {
	if app := os.Getenv("APPDATA"); app != "" && filepath.IsAbs(app) {
		return app, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "AppData", "Roaming"), nil
}
