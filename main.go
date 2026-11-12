package main

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/maci0/subman/internal/app"
	"github.com/maci0/subman/internal/cli"
	"github.com/maci0/subman/internal/desktop"
	"github.com/maci0/subman/internal/sandbox"
	"github.com/maci0/subman/internal/store"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

// version is the release this binary reports. A tag drops the leading v.
var version = "0.1.0"

// wantsCLI reports whether the arguments are a command rather than a
// request to open the window. The command line follows CLI Spec v0.3.
func wantsCLI(args []string) bool {
	for _, a := range args {
		switch a {
		case "--output", "-o", "--help", "-h", "help", "version", "--version", "-v",
			"schema", "accounts", "ladder", "update":
			return true
		}
		if strings.HasPrefix(a, "--output=") {
			return true
		}
	}
	return false
}

func main() {
	if sandbox.MaybeReexec(os.Args) {
		return
	}
	if wantsCLI(os.Args[1:]) {
		cli.Version = version
		os.Exit(cli.Run(os.Args[1:], cli.Std()))
	}

	data := flag.String("data", os.Getenv("SUBMAN_DATA"), "data directory (default: $SUBMAN_DATA, then $XDG_DATA_HOME/subman)")
	flag.Parse()

	root, err := store.Open(*data)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	app.Version = version
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
// the page's own background for the desktop's scheme (--background in
// frontend/src/index.css), so the window does not flash the other theme.
// A desktop whose preference cannot be read gets light, the page's default.
func background() *options.RGBA {
	if dark, err := desktop.PrefersDark(); err == nil && dark {
		return &options.RGBA{R: 4, G: 25, B: 30, A: 255}
	}
	return &options.RGBA{R: 241, G: 248, B: 249, A: 255}
}
