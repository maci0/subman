//go:build !linux

package desktop

// ponytail: only the freedesktop session is wired. Add the macOS and Windows
// calls when a release for them needs them.

// Notify returns ErrUnsupported.
func Notify(title, body string) error {
	return ErrUnsupported
}

// PrefersDark returns ErrUnsupported.
func PrefersDark() (bool, error) {
	return false, ErrUnsupported
}
