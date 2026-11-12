//go:build windows

package update

import "os"

// replaceFile puts src where dest is. Windows denies a rename onto an
// executable that is still running, which dest is: this process is it.
// Renaming dest aside is permitted, and src can then take the freed name.
// If that second rename fails, dest is moved back so the binary that was
// running is still the one on disk.
func replaceFile(src, dest string) error {
	aside := dest + ".old"
	os.Remove(aside)
	if err := os.Rename(dest, aside); err != nil {
		if os.IsNotExist(err) {
			return os.Rename(src, dest)
		}
		return err
	}
	if err := os.Rename(src, dest); err != nil {
		os.Rename(aside, dest)
		return err
	}
	os.Remove(aside)
	return nil
}
