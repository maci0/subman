// Package version is the one place the release number is declared.
//
// Both binaries set it before they serve a request: the window in main, the
// CLI in cmd/subman. A release build overrides it with -X, so the two cannot
// be stamped apart and the packages that report it cannot keep an older
// number. A build that was not stamped reports dev, never a release number
// this tree no longer is.
package version

// Release is the release this binary reports, compared against the latest
// published tag by the update check. A tag drops the leading v.
var Release = "dev"
