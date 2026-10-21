package gitcmd

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// MinGitVersion is the oldest git release metagit relies on. Older git
// ignores GIT_CONFIG_GLOBAL and reads the user config of the service user.
var MinGitVersion = [2]int{2, 32}

// CheckTools verifies that git (of a supported version) and the ssh client
// are installed. It returns the git version on success.
func CheckTools(ctx context.Context, gitBin, sshBin string) (string, error) {
	gitPath, err := exec.LookPath(gitBin)
	if err != nil {
		return "", fmt.Errorf("git is not found (%s): install git %d.%d or newer", gitBin, MinGitVersion[0], MinGitVersion[1])
	}
	out, err := exec.CommandContext(ctx, gitPath, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("cannot run %s --version: %w", gitPath, err)
	}
	version := strings.TrimSpace(string(out))
	major, minor, err := ParseGitVersion(version)
	if err != nil {
		return "", err
	}
	if major < MinGitVersion[0] || (major == MinGitVersion[0] && minor < MinGitVersion[1]) {
		return "", fmt.Errorf("%s is too old: metagit needs git %d.%d or newer", version, MinGitVersion[0], MinGitVersion[1])
	}
	sshPath, err := exec.LookPath(sshBin)
	if err != nil {
		return "", fmt.Errorf("ssh client is not found (%s): install OpenSSH client, it is used to deliver to SSH targets", sshBin)
	}
	if err := exec.CommandContext(ctx, sshPath, "-V").Run(); err != nil {
		return "", fmt.Errorf("cannot run %s -V: %w", sshPath, err)
	}
	return version, nil
}

// ParseGitVersion parses "git version 2.55.0" (also "2.39.5 (Apple Git-154)").
func ParseGitVersion(s string) (major, minor int, err error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(s), "git version ")
	if !ok {
		return 0, 0, fmt.Errorf("unexpected git version output %q", s)
	}
	fields := strings.SplitN(strings.Fields(rest)[0], ".", 3)
	if len(fields) < 2 {
		return 0, 0, fmt.Errorf("unexpected git version output %q", s)
	}
	if major, err = strconv.Atoi(fields[0]); err != nil {
		return 0, 0, fmt.Errorf("unexpected git version output %q", s)
	}
	if minor, err = strconv.Atoi(fields[1]); err != nil {
		return 0, 0, fmt.Errorf("unexpected git version output %q", s)
	}
	return major, minor, nil
}
