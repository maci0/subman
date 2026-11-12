// Package desktop talks to the desktop session: it shows a notification and
// reads the light or dark preference.
package desktop

import "errors"

// ErrUnsupported is returned on a platform this package has no code for.
var ErrUnsupported = errors.New("not supported on this platform")
