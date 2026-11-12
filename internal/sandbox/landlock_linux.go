//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Landlock, as gauntlet and microagent apply it: a ruleset that handles the
// filesystem rights this kernel knows, a read-only grant on /, and a
// read-write grant on the account's agent directory. No user namespace is
// entered. A process with no capabilities and no_new_privs already set,
// which is what a sandboxed agent session is, cannot unshare(CLONE_NEWUSER).

const landlockCreateVersion uintptr = 1 << 0

func landlockABI() (uintptr, error) {
	abi, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return 0, fmt.Errorf("landlock unavailable: %w", errno)
	}
	return abi, nil
}

func landlockAccess(abi uintptr) uint64 {
	rights := uint64((1 << 13) - 1)
	if abi >= 2 {
		rights |= unix.LANDLOCK_ACCESS_FS_REFER
	}
	if abi >= 3 {
		rights |= unix.LANDLOCK_ACCESS_FS_TRUNCATE
	}
	if abi >= 5 {
		rights |= unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	}
	return rights
}

// confine applies the ruleset to this process. It is called in the child
// after a re-exec, so the parent stays unconfined.
func confine(writable string) error {
	abi, err := landlockABI()
	if err != nil {
		return err
	}
	handled := landlockAccess(abi)
	// The kernel rejects a size other than the one it knows for this ABI
	// (EINVAL below it, E2BIG above it), so the literal 8 that matched the
	// original one-field struct fails once handled_access_net and scoped
	// exist. The trailing fields are zero, which handles nothing.
	attr := unix.LandlockRulesetAttr{Access_fs: handled}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return fmt.Errorf("landlock ruleset: %w", errno)
	}
	defer unix.Close(int(fd))

	add := func(path string, access uint64, directory bool) error {
		flags := unix.O_PATH | unix.O_CLOEXEC
		if directory {
			flags |= unix.O_DIRECTORY
		}
		parent, err := unix.Open(path, flags, 0)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		defer unix.Close(parent)
		rule := unix.LandlockPathBeneathAttr{Allowed_access: access, Parent_fd: int32(parent)}
		_, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, fd, unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&rule)), 0, 0, 0)
		if errno != 0 {
			return fmt.Errorf("landlock rule %s: %w", path, errno)
		}
		return nil
	}

	read := uint64(unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR)
	if err := add("/", read, true); err != nil {
		return err
	}
	if err := add(writable, handled, true); err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("no_new_privs: %w", err)
	}
	_, _, errno = unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0)
	if errno != 0 {
		return fmt.Errorf("landlock restrict: %w", errno)
	}
	return nil
}

// applyChild is the re-exec entry. SUBMAN_SANDBOX names the one writable
// directory. The agent command follows in SUBMAN_AGENT_ARG0 and the rest
// of the environment is the child's own.
func applyChild() error {
	dir := os.Getenv("SUBMAN_SANDBOX")
	if dir == "" {
		return fmt.Errorf("SUBMAN_SANDBOX is empty")
	}
	return confine(dir)
}
