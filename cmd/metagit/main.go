// Command metagit is a self-hosted git remote that mirrors branches and tags
// to several git hostings.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ekalinin/metagit/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, os.Args[1:], cli.OSEnv())
	stop()
	os.Exit(code)
}
