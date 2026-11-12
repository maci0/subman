//go:build !linux

package sandbox

import "fmt"

func landlockABI() (uintptr, error) {
	return 0, fmt.Errorf("landlock is linux only")
}

func applyChild() error {
	return fmt.Errorf("landlock is linux only")
}
