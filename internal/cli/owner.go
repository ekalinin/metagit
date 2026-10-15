package cli

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/ekalinin/metagit/internal/auth"
	"github.com/ekalinin/metagit/internal/config"
	"github.com/ekalinin/metagit/internal/store"
)

func runOwnerCreate(ctx context.Context, args []string, env Env) int {
	fs := flag.NewFlagSet("owner create", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	dataDir := fs.String("data-dir", cmp.Or(env.Getenv("METAGIT_DATA_DIR"), config.DefaultDataDir),
		"directory with repositories, database and keys (env METAGIT_DATA_DIR)")
	name := fs.String("name", "", "owner name (required)")
	fs.Usage = func() {
		fmt.Fprintln(env.Stderr, "usage: metagit owner create --name <name> [--data-dir <dir>]")
		fmt.Fprintln(env.Stderr, "Creates the owner or, for the existing owner, changes the password.")
		fmt.Fprintln(env.Stderr, "The password is asked on the terminal or read from the first line of stdin.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *name == "" || fs.NArg() > 0 {
		fs.Usage()
		return 2
	}
	password, err := readPassword(env)
	if err != nil {
		fmt.Fprintln(env.Stderr, "error:", err)
		return 1
	}
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		fmt.Fprintln(env.Stderr, "error:", err)
		return 1
	}
	st, err := store.Open(ctx, config.DBPath(*dataDir))
	if err != nil {
		fmt.Fprintln(env.Stderr, "error:", err)
		return 1
	}
	defer st.Close()
	created, err := auth.New(st).CreateOwner(ctx, *name, password)
	if err != nil {
		fmt.Fprintln(env.Stderr, "error:", err)
		return 1
	}
	if created {
		fmt.Fprintf(env.Stdout, "owner %q created\n", *name)
	} else {
		fmt.Fprintf(env.Stdout, "password of owner %q changed\n", *name)
	}
	return 0
}

// readPassword asks twice on a terminal, otherwise reads the first line.
func readPassword(env Env) (string, error) {
	if f, ok := env.Stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(env.Stderr, "Password: ")
		first, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(env.Stderr)
		if err != nil {
			return "", err
		}
		fmt.Fprint(env.Stderr, "Repeat password: ")
		second, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(env.Stderr)
		if err != nil {
			return "", err
		}
		if string(first) != string(second) {
			return "", errors.New("passwords do not match")
		}
		return string(first), nil
	}
	line, err := bufio.NewReader(env.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return "", errors.New("empty password on stdin")
	}
	return line, nil
}
