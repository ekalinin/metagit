package replicate

import (
	"fmt"
	"io"
	"strings"
)

// Environment variables that pass HTTPS credentials to "metagit askpass".
const (
	EnvAskpassUsername = "METAGIT_ASKPASS_USERNAME"
	EnvAskpassPassword = "METAGIT_ASKPASS_PASSWORD"
)

// Askpass answers a git credential prompt ("Username for ...", "Password
// for ...") from the environment. It returns the exit code.
func Askpass(prompt string, getenv func(string) string, stdout io.Writer) int {
	var value string
	switch {
	case strings.HasPrefix(prompt, "Username"):
		value = getenv(EnvAskpassUsername)
	case strings.HasPrefix(prompt, "Password"):
		value = getenv(EnvAskpassPassword)
	default:
		return 1
	}
	if value == "" {
		return 1
	}
	fmt.Fprintln(stdout, value)
	return 0
}
