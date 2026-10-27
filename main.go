package main

import (
	"os"

	"github.com/rknit/ssmux/internal/app"
	"github.com/rknit/ssmux/internal/tmux"
	"github.com/rknit/ssmux/internal/ui"
)

func main() {
	app.RunPopup = func(c tmux.Client, client string) error { return ui.Run(c, client) }
	os.Exit(app.Main(os.Args[1:]))
}
