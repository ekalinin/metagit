// Package gitcmd runs the system git binary.
package gitcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ZeroOID reports whether oid is the all-zero object id git uses for
// "no object" (works for both SHA-1 and SHA-256 repositories).
func ZeroOID(oid string) bool {
	return oid != "" && strings.Trim(oid, "0") == ""
}

// Runner runs git commands.
type Runner struct {
	// Git is the git binary, "git" when empty.
	Git string
	// Timeout limits every command unless the context already has a deadline.
	Timeout time.Duration
	// Env is the environment of every command. When nil, CleanEnv() is used.
	Env []string
}

// Opts describe a single command.
type Opts struct {
	Dir   string
	Env   []string // added to the runner environment
	Stdin io.Reader
}

// Result is the output of a finished command.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Error is returned when git exits with a non-zero code or cannot start.
type Error struct {
	Args     []string
	ExitCode int
	Stderr   string
	Err      error
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" && e.Err != nil {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("git %s: exit code %d: %s", strings.Join(e.Args, " "), e.ExitCode, msg)
}

func (e *Error) Unwrap() error { return e.Err }

// CleanEnv returns the process environment without GIT_* variables and with
// settings that make git output stable and independent from user config.
func CleanEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_") || strings.HasPrefix(kv, "LC_ALL=") || strings.HasPrefix(kv, "LANG=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"LC_ALL=C",
		"LANG=C",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
	)
}

func (r *Runner) git() string {
	if r.Git == "" {
		return "git"
	}
	return r.Git
}

// Run runs git with args. On a non-zero exit it returns both the result and
// an *Error, so callers can parse output of partially failed commands.
func (r *Runner) Run(ctx context.Context, o Opts, args ...string) (*Result, error) {
	if _, ok := ctx.Deadline(); !ok && r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, r.git(), args...)
	cmd.Dir = o.Dir
	env := r.Env
	if env == nil {
		env = CleanEnv()
	}
	cmd.Env = append(append([]string(nil), env...), o.Env...)
	cmd.Stdin = o.Stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	res := &Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err == nil {
		return res, nil
	}
	gerr := &Error{Args: args, ExitCode: -1, Stderr: stderr.String(), Err: err}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		gerr.ExitCode = exitErr.ExitCode()
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		gerr.Err = ctxErr
	}
	res.ExitCode = gerr.ExitCode
	return res, gerr
}

// Output runs git and returns trimmed stdout.
func (r *Runner) Output(ctx context.Context, o Opts, args ...string) (string, error) {
	res, err := r.Run(ctx, o, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// Ref is a reference and the object it points to.
type Ref struct {
	Name   string
	Object string
}

// ForEachRef lists refs under the given prefixes (for example "refs/heads/").
func (r *Runner) ForEachRef(ctx context.Context, gitDir string, prefixes ...string) ([]Ref, error) {
	args := append([]string{"for-each-ref", "--format=%(objectname) %(refname)"}, prefixes...)
	res, err := r.Run(ctx, Opts{Dir: gitDir}, args...)
	if err != nil {
		return nil, err
	}
	return ParseForEachRef(res.Stdout)
}

// ParseForEachRef parses "<object> <refname>" lines.
func ParseForEachRef(out []byte) ([]Ref, error) {
	var refs []Ref
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		obj, name, ok := strings.Cut(line, " ")
		if !ok || obj == "" || name == "" {
			return nil, fmt.Errorf("unexpected for-each-ref line %q", line)
		}
		refs = append(refs, Ref{Name: name, Object: obj})
	}
	return refs, nil
}

// IsAncestor reports whether ancestor is reachable from descendant.
func (r *Runner) IsAncestor(ctx context.Context, o Opts, ancestor, descendant string) (bool, error) {
	_, err := r.Run(ctx, o, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	if gerr, ok := errors.AsType[*Error](err); ok && gerr.ExitCode == 1 {
		return false, nil
	}
	return false, err
}

// HasObject reports whether the repository contains the object.
func (r *Runner) HasObject(ctx context.Context, gitDir, oid string) bool {
	_, err := r.Run(ctx, Opts{Dir: gitDir}, "cat-file", "-e", oid)
	return err == nil
}

// ResolveRef returns the object a ref points to, or "" when it does not exist.
func (r *Runner) ResolveRef(ctx context.Context, gitDir, ref string) (string, error) {
	res, err := r.Run(ctx, Opts{Dir: gitDir}, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		if gerr, ok := errors.AsType[*Error](err); ok && gerr.ExitCode == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}
