package cli

import (
	"github.com/ekalinin/metagit/internal/replicate"
)

// runAskpass is called by git through GIT_ASKPASS with the prompt as the
// only argument.
func runAskpass(args []string, env Env) int {
	if len(args) != 1 {
		return 2
	}
	return replicate.Askpass(args[0], env.Getenv, env.Stdout)
}
