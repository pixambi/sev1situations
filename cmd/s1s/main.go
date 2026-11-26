// Command s1s is the Sev1 Situations executable. With no arguments it opens the
// application window. With arguments it runs a headless subcommand (R-APP-5).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	sev1situations "github.com/pixambi/sev1situations"
	"github.com/pixambi/sev1situations/internal/app"
	"github.com/pixambi/sev1situations/internal/gui"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) > 1 {
		code := app.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, app.Options{Builtin: sev1situations.Scenarios})
		stop()
		os.Exit(code)
	}

	if err := gui.Run(sev1situations.Frontend); err != nil {
		fmt.Fprintln(os.Stderr, "s1s:", err)
		os.Exit(1)
	}
}
