package main

import (
	"os"

	"github.com/rknit/ssmux/internal/app"
	"github.com/rknit/ssmux/internal/fleet"
	"github.com/rknit/ssmux/internal/ui"
)

func main() {
	app.RunPopup = func(f *fleet.Fleet, client string) error { return ui.Run(f, client) }
	os.Exit(app.Main(os.Args[1:]))
}
