//go:build linux

package desktop

import (
	"context"
	"time"

	"github.com/godbus/dbus/v5"
)

// callTimeout bounds one D-Bus call, so a hung notification daemon or portal
// cannot stall a poll or the window's start.
const callTimeout = 2 * time.Second

// Notify asks the session's notification daemon to show title and body, over
// org.freedesktop.Notifications. It fails when there is no session bus or no
// daemon, or when the daemon does not answer within callTimeout; it does not
// wait for the notification to be read.
func Notify(title, body string) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return err
	}
	const (
		noReplace      = uint32(0)
		defaultTimeout = int32(-1)
	)
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	return obj.CallWithContext(ctx, "org.freedesktop.Notifications.Notify", 0,
		"subman", noReplace, "", title, body, []string{}, map[string]dbus.Variant{}, defaultTimeout).Err
}

// portalPrefersDark is the org.freedesktop.appearance color-scheme value for dark.
const portalPrefersDark = uint32(1)

// PrefersDark reports whether the desktop asks applications for a dark
// appearance, read from the settings portal. It fails when there is no
// session bus or no portal, or when the portal does not answer within
// callTimeout.
func PrefersDark() (bool, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var value dbus.Variant
	obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	err = obj.CallWithContext(ctx, "org.freedesktop.portal.Settings.Read", 0, "org.freedesktop.appearance", "color-scheme").Store(&value)
	if err != nil {
		return false, err
	}
	// Read wraps the setting in a second variant.
	inner := value.Value()
	if wrapped, ok := inner.(dbus.Variant); ok {
		inner = wrapped.Value()
	}
	scheme, ok := inner.(uint32)
	return ok && scheme == portalPrefersDark, nil
}
