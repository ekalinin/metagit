package gitssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/ekalinin/metagit/internal/gitcmd"
	"github.com/ekalinin/metagit/internal/repo"
	"github.com/ekalinin/metagit/internal/testutil"
)

type noPushes struct{}

func (noPushes) NewPush(string) ([]string, func()) { return nil, func() {} }

type clientKey struct {
	signer ssh.Signer
	path   string
}

func newClientKey(t *testing.T) clientKey {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return clientKey{signer: signer, path: path}
}

type env struct {
	addr  string
	repos *repo.Manager
	owner clientKey
}

func setup(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	repos := &repo.Manager{ReposDir: filepath.Join(dir, "repos"), HooksDir: filepath.Join(dir, "hooks"), Git: &gitcmd.Runner{}}
	if err := repos.Create(t.Context(), "foo"); err != nil {
		t.Fatal(err)
	}
	hostKey, err := LoadOrCreateHostKey(filepath.Join(dir, "ssh_host_ed25519_key"))
	if err != nil {
		t.Fatal(err)
	}
	owner := newClientKey(t)
	srv := &Server{
		ReposDir: repos.ReposDir,
		HostKey:  hostKey,
		CheckKey: func(_ context.Context, key ssh.PublicKey) bool {
			return string(key.Marshal()) == string(owner.signer.PublicKey().Marshal())
		},
		ProjectExists: func(_ context.Context, name string) bool { return repos.Exists(name) },
		Pushes:        noPushes{},
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	return &env{addr: l.Addr().String(), repos: repos, owner: owner}
}

func sshCommand(key string) string {
	return fmt.Sprintf("ssh -F /dev/null -i %s -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR", key)
}

func (e *env) url(project string) string {
	return "ssh://git@" + e.addr + "/" + project + ".git"
}

func TestCloneAndPushOverSSH(t *testing.T) {
	e := setup(t)
	gitEnv := []string{"GIT_SSH_COMMAND=" + sshCommand(e.owner.path)}
	work := testutil.InitWork(t)
	c := testutil.Commit(t, work, "first")
	if out, err := testutil.GitErr(work, gitEnv, "push", "-q", e.url("foo"), "main"); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}
	if got := testutil.RefOf(t, e.repos.Path("foo"), "refs/heads/main"); got != c {
		t.Fatalf("main = %q, want %q", got, c)
	}
	clone := filepath.Join(t.TempDir(), "clone")
	if out, err := testutil.GitErr(t.TempDir(), gitEnv, "clone", "-q", e.url("foo"), clone); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	if got := testutil.RefOf(t, clone, "HEAD"); got != c {
		t.Fatalf("clone HEAD = %q", got)
	}
	// Protocol v2 passes GIT_PROTOCOL through an "env" request.
	if out, err := testutil.GitErr(clone, gitEnv, "-c", "protocol.version=2", "fetch", "-q"); err != nil {
		t.Fatalf("fetch v2: %v\n%s", err, out)
	}
	out, err := testutil.GitErr(t.TempDir(), gitEnv, "clone", e.url("nope"), "x")
	if err == nil || !strings.Contains(out, "not found") {
		t.Fatalf("unknown project: %v\n%s", err, out)
	}
}

func TestUnregisteredKeyRejected(t *testing.T) {
	e := setup(t)
	stranger := newClientKey(t)
	out, err := testutil.GitErr(t.TempDir(), []string{"GIT_SSH_COMMAND=" + sshCommand(stranger.path)}, "clone", e.url("foo"), "x")
	if err == nil || !strings.Contains(out, "Permission denied") {
		t.Fatalf("clone with unknown key: %v\n%s", err, out)
	}
	_, err = ssh.Dial("tcp", e.addr, &ssh.ClientConfig{
		User:            "git",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(stranger.signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err == nil {
		t.Fatal("handshake with unknown key must fail")
	}
}

func TestShellAndOtherCommandsRejected(t *testing.T) {
	e := setup(t)
	client, err := ssh.Dial("tcp", e.addr, &ssh.ClientConfig{
		User:            "git",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(e.owner.signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	s, _ := client.NewSession()
	if err := s.RequestPty("xterm", 40, 80, ssh.TerminalModes{}); err == nil {
		t.Error("pty request must be refused")
	}
	if err := s.Shell(); err == nil {
		t.Error("shell request must be refused")
	}
	s.Close()

	for _, cmd := range []string{"ls /", "git-upload-archive 'foo.git'", "git-upload-pack '../etc.git'", "sh -c 'id'"} {
		s, _ := client.NewSession()
		if err := s.Run(cmd); err == nil {
			t.Errorf("%q must be refused", cmd)
		}
		s.Close()
	}
}

func TestParseCommand(t *testing.T) {
	ok := map[string][2]string{
		"git-upload-pack 'foo.git'":    {"git-upload-pack", "foo"},
		"git-receive-pack '/foo.git'":  {"git-receive-pack", "foo"},
		"git upload-pack '~/foo.git'":  {"git-upload-pack", "foo"},
		"git receive-pack 'my-p.git/'": {"git-receive-pack", "my-p"},
		"git-upload-pack foo":          {"git-upload-pack", "foo"},
	}
	for cmd, want := range ok {
		service, project, err := ParseCommand(cmd)
		if err != nil || service != want[0] || project != want[1] {
			t.Errorf("%q: %q %q %v", cmd, service, project, err)
		}
	}
	for _, bad := range []string{"", "ls", "git-upload-archive 'foo.git'", "git-upload-pack '../foo.git'",
		"git-upload-pack 'a/b.git'", "git-upload-pack 'foo.git'; rm -rf /", "git-upload-pack 'Foo.git'"} {
		if _, _, err := ParseCommand(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestHostKeyIsReused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	k1, err := LoadOrCreateHostKey(path)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := LoadOrCreateHostKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(k1.PublicKey().Marshal()) != string(k2.PublicKey().Marshal()) {
		t.Fatal("host key changed between starts")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("host key mode %v", info.Mode().Perm())
	}
}
