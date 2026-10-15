package cli

import (
	"context"
	"fmt"

	"github.com/ekalinin/metagit/internal/hooks"
)

func runHook(ctx context.Context, args []string, env Env) int {
	if len(args) != 1 {
		fmt.Fprintln(env.Stderr, "usage: metagit hook <pre-receive|post-receive>")
		return 2
	}
	return hooks.RunHook(ctx, args[0], env.Stdin, env.Stderr, env.Getenv)
}
