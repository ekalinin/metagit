// Package cli implements metagit subcommands.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
)

// Env is what a subcommand gets from the process.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
}

// OSEnv returns Env bound to the current process.
func OSEnv() Env {
	return Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Getenv: os.Getenv}
}

const usage = `metagit - self-hosted git remote that mirrors branches and tags to several hostings

Usage:
  metagit <command> [flags]

Commands:
  serve          run the service (web interface, API, git over HTTP and SSH)
  owner create   create the owner or change the owner's password
  hook           called by git hooks of metagit repositories
  askpass        gives hosting credentials to git (internal)
  version        show the metagit version
  help           show this help

Run "metagit <command> --help" for command flags.
`

// Run executes the subcommand from args and returns the process exit code.
func Run(ctx context.Context, args []string, env Env) int {
	if len(args) == 0 {
		fmt.Fprint(env.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(env.Stdout, usage)
		return 0
	case "version", "--version":
		fmt.Fprintln(env.Stdout, "metagit", versionString())
		return 0
	case "serve":
		return runServe(ctx, args[1:], env)
	case "owner":
		if len(args) < 2 || args[1] != "create" {
			fmt.Fprintln(env.Stderr, `usage: metagit owner create [flags]`)
			return 2
		}
		return runOwnerCreate(ctx, args[2:], env)
	case "hook":
		return runHook(ctx, args[1:], env)
	case "askpass":
		return runAskpass(args[1:], env)
	default:
		fmt.Fprintf(env.Stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}
