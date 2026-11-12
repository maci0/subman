package main

import (
	"embed"
	"flag"
	"fmt"
	"os"

	"github.com/maci0/subman/internal/app"
	"github.com/maci0/subman/internal/cli"
	"github.com/maci0/subman/internal/desktop"
	"github.com/maci0/subman/internal/sandbox"
	"github.com/maci0/subman/internal/store"
	"github.com/maci0/subman/internal/version"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

// release is the release this binary reports. A tag drops the leading v.
// The default is what an unstamped build reports; the release workflow
// overrides it with -X main.release, and the same value is copied into
// version.Release so the CLI and the window report it too.
var release = "dev"

// wantsCLI reports whether the arguments are a command rather than a
// request to open the window. The command line follows CLI Spec v0.3.
func wantsCLI(args []string) bool {
	return cli.WantsCLI(args)
}

func main() {
	if sandbox.MaybeReexec(os.Args) {
		return
	}
	version.Release = release
	if wantsCLI(os.Args[1:]) {
		cli.Version = release
		os.Exit(cli.Run(os.Args[1:], cli.Std()))
	}

	data := flag.String("data", os.Getenv("SUBMAN_DATA"), "data directory (default: $SUBMAN_DATA, then $XDG_DATA_HOME/subman, then the platform's own per-user data directory)")
	flag.Parse()

	root, err := store.Open(*data)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	app.Version = release
	application := app.New(root)

	err = wails.Run(&options.App{
		Title:     "subman",
		Width:     1280,
		Height:    840,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: background(),
		OnStartup:        application.Startup,
		Bind:             []any{application},
		Linux: &linux.Options{
			ProgramName: "subman",
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// background is the colour the frame is painted in before the page loads:
// the page's own background for the desktop's scheme (--color-bg in
// frontend/src/system/tokens.css), so the window does not flash the other theme.
// A desktop whose preference cannot be read gets light, the page's default.
func background() *options.RGBA {
	if dark, err := desktop.PrefersDark(); err == nil && dark {
		return &options.RGBA{R: 11, G: 11, B: 12, A: 255}
	}
	return &options.RGBA{R: 250, G: 250, B: 250, A: 255}
}
