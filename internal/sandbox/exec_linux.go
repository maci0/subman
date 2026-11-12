//go:build linux

package sandbox

import "golang.org/x/sys/unix"

func unixExec(argv0 string, argv []string, env []string) error {
	return unix.Exec(argv0, argv, env)
}
