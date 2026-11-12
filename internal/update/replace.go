//go:build !windows

package update

import "os"

// replaceFile puts src where dest is. src and dest are in the same directory,
// so the rename does not cross a filesystem.
func replaceFile(src, dest string) error {
	return os.Rename(src, dest)
}
