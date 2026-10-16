package hooks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ekalinin/metagit/internal/gitcmd"
)

// ClientTimeout limits a hook request. The service keeps post-receive within
// its push status timeout, so this is only a safety net.
var ClientTimeout = 5 * time.Minute

// RunHook is the body of "metagit hook <name>". It reads ref updates from
// stdin, asks the service and returns the exit code for git.
func RunHook(ctx context.Context, hook string, stdin io.Reader, stderr io.Writer, getenv func(string) string) int {
	if hook != PreReceive && hook != PostReceive {
		fmt.Fprintf(stderr, "metagit: unknown hook %q\n", hook)
		return 1
	}
	socket, project, pushID := getenv(EnvSocket), getenv(EnvProject), getenv(EnvPushID)
	if socket == "" || project == "" || pushID == "" {
		fmt.Fprintln(stderr, "metagit: push bypasses metagit: the repository accepts changes only through the metagit service")
		return 1
	}
	updates, err := ParseUpdates(stdin)
	if err != nil {
		fmt.Fprintln(stderr, "metagit:", err)
		return 1
	}
	if hook == PreReceive {
		// The hook process inherits the quarantine environment from git,
		// so only here new objects are visible for the ancestry check.
		runner := &gitcmd.Runner{Env: os.Environ()}
		for i := range updates {
			updates[i].Kind, err = Classify(ctx, runner, updates[i])
			if err != nil {
				fmt.Fprintln(stderr, "metagit:", err)
				return 1
			}
		}
	}
	resp, err := call(ctx, socket, Request{Hook: hook, PushID: pushID, Project: project, Updates: updates})
	if err != nil {
		if hook == PreReceive {
			fmt.Fprintln(stderr, "metagit: service unavailable, push rejected:", err)
			return 1
		}
		fmt.Fprintln(stderr, "metagit: cannot get the delivery status:", err)
		return 0
	}
	for _, m := range resp.Messages {
		fmt.Fprintln(stderr, m)
	}
	if hook == PreReceive && !resp.Allow {
		return 1
	}
	return 0
}

// ParseUpdates reads "<old> <new> <ref>" lines.
func ParseUpdates(r io.Reader) ([]Update, error) {
	var updates []Update
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 3 {
			return nil, fmt.Errorf("unexpected hook input %q", line)
		}
		updates = append(updates, Update{Old: f[0], New: f[1], Ref: f[2]})
	}
	return updates, sc.Err()
}

// Classify determines the kind of an update. runner must see the objects of
// the push (in pre-receive: the inherited quarantine environment).
func Classify(ctx context.Context, runner *gitcmd.Runner, u Update) (Kind, error) {
	switch {
	case gitcmd.ZeroOID(u.Old):
		return KindCreate, nil
	case gitcmd.ZeroOID(u.New):
		return KindDelete, nil
	case strings.HasPrefix(u.Ref, "refs/tags/"):
		return KindTagMove, nil
	}
	ok, err := runner.IsAncestor(ctx, gitcmd.Opts{}, u.Old, u.New)
	if err != nil {
		// merge-base fails for non-commit objects: treat the update as
		// rewriting history, which is the safe side.
		if _, isGit := errors.AsType[*gitcmd.Error](err); isGit {
			return KindNonFastForward, nil
		}
		return "", err
	}
	if ok {
		return KindFastForward, nil
	}
	return KindNonFastForward, nil
}

func call(ctx context.Context, socket string, req Request) (*Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout: ClientTimeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
		},
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://metagit/hook", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	res, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(msg)))
	}
	var resp Response
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
