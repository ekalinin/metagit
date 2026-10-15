// Package gitssh is an SSH server that only runs git upload-pack and
// receive-pack for metagit projects.
package gitssh

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ekalinin/metagit/internal/gitcmd"
	"github.com/ekalinin/metagit/internal/repo"
)

// Pushes registers git invocations so that hooks can reach the service.
type Pushes interface {
	NewPush(project string) (env []string, done func())
}

// Server serves git over SSH.
type Server struct {
	ReposDir      string
	Git           string
	HostKey       ssh.Signer
	CheckKey      func(ctx context.Context, key ssh.PublicKey) bool
	ProjectExists func(ctx context.Context, name string) bool
	Pushes        Pushes
	Log           *slog.Logger

	mu       sync.Mutex
	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// LoadOrCreateHostKey reads an ed25519 host key, creating it when missing.
func LoadOrCreateHostKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		block, err := ssh.MarshalPrivateKey(priv, "metagit host key")
		if err != nil {
			return nil, err
		}
		data = pem.EncodeToMemory(block)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(data)
}

// Serve accepts connections until Close is called.
func (s *Server) Serve(l net.Listener) error {
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if s.CheckKey(context.Background(), key) {
				return &ssh.Permissions{}, nil
			}
			return nil, errors.New("unknown public key")
		},
		ServerVersion: "SSH-2.0-metagit",
	}
	config.AddHostKey(s.HostKey)

	s.mu.Lock()
	s.listener = l
	s.ctx, s.cancel = context.WithCancel(context.Background())
	ctx := s.ctx
	s.mu.Unlock()

	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if ne, ok := errors.AsType[net.Error](err); ok && ne.Timeout() {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return err
		}
		s.wg.Go(func() { s.handleConn(ctx, conn, config) })
	}
}

// Close stops accepting connections and aborts running git processes.
func (s *Server) Close() error {
	s.mu.Lock()
	l, cancel := s.listener, s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	var err error
	if l != nil {
		err = l.Close()
	}
	s.wg.Wait()
	return err
}

func (s *Server) handleConn(ctx context.Context, nConn net.Conn, config *ssh.ServerConfig) {
	defer nConn.Close()
	_ = nConn.SetDeadline(time.Now().Add(30 * time.Second))
	conn, chans, reqs, err := ssh.NewServerConn(nConn, config)
	if err != nil {
		s.Log.Debug("ssh handshake failed", "remote", nConn.RemoteAddr(), "err", err)
		return
	}
	_ = nConn.SetDeadline(time.Time{})
	defer conn.Close()
	go ssh.DiscardRequests(reqs)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	var wg sync.WaitGroup
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		ch, requests, err := newCh.Accept()
		if err != nil {
			continue
		}
		wg.Go(func() { s.handleSession(ctx, ch, requests) })
	}
	wg.Wait()
}

type exitStatus struct{ Status uint32 }

func (s *Server) handleSession(ctx context.Context, ch ssh.Channel, requests <-chan *ssh.Request) {
	defer ch.Close()
	var gitProtocol string
	for req := range requests {
		switch req.Type {
		case "env":
			var kv struct{ Name, Value string }
			if ssh.Unmarshal(req.Payload, &kv) == nil && kv.Name == "GIT_PROTOCOL" {
				gitProtocol = kv.Value
				_ = req.Reply(true, nil)
				continue
			}
			_ = req.Reply(false, nil)
		case "exec":
			var payload struct{ Command string }
			if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
				_ = req.Reply(false, nil)
				continue
			}
			service, project, err := ParseCommand(payload.Command)
			if err != nil {
				s.Log.Info("ssh command rejected", "command", payload.Command)
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			go ssh.DiscardRequests(requests)
			code := s.runGit(ctx, ch, service, project, gitProtocol)
			_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(exitStatus{code}))
			return
		default:
			// shell, pty-req, subsystem and anything else is refused.
			_ = req.Reply(false, nil)
		}
	}
}

func (s *Server) runGit(ctx context.Context, ch ssh.Channel, service, project, gitProtocol string) uint32 {
	if !s.ProjectExists(ctx, project) {
		fmt.Fprintf(ch.Stderr(), "fatal: repository %s.git not found\n", project)
		return 1
	}
	env, done := s.Pushes.NewPush(project)
	defer done()
	env = append(gitcmd.CleanEnv(), env...)
	if gitProtocol != "" {
		env = append(env, "GIT_PROTOCOL="+gitProtocol)
	}
	cmd := exec.CommandContext(ctx, cmp.Or(s.Git, "git"), strings.TrimPrefix(service, "git-"), filepath.Join(s.ReposDir, project+".git"))
	cmd.Env = env
	cmd.Stdout = ch
	cmd.Stderr = ch.Stderr()
	cmd.WaitDelay = 5 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		fmt.Fprintln(ch.Stderr(), "fatal: internal error")
		return 1
	}
	if err := cmd.Start(); err != nil {
		s.Log.Error("cannot start git", "err", err)
		fmt.Fprintln(ch.Stderr(), "fatal: internal error")
		return 1
	}
	// Do not wait for this copy: the client keeps its side open until the
	// server closes the channel.
	go func() {
		_, _ = io.Copy(stdin, ch)
		stdin.Close()
	}()
	err = cmd.Wait()
	if err == nil {
		return 0
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() > 0 {
		return uint32(exitErr.ExitCode())
	}
	return 1
}

// ParseCommand parses the command a git client sends over SSH and returns
// the git service ("git-upload-pack" or "git-receive-pack") and the project.
func ParseCommand(command string) (service, project string, err error) {
	command = strings.TrimSpace(command)
	var arg string
	switch {
	case strings.HasPrefix(command, "git-upload-pack "), strings.HasPrefix(command, "git-receive-pack "):
		service, arg, _ = strings.Cut(command, " ")
	case strings.HasPrefix(command, "git upload-pack "), strings.HasPrefix(command, "git receive-pack "):
		rest := strings.TrimPrefix(command, "git ")
		var sub string
		sub, arg, _ = strings.Cut(rest, " ")
		service = "git-" + sub
	default:
		return "", "", fmt.Errorf("command %q is not allowed", command)
	}
	arg = strings.TrimSpace(arg)
	if len(arg) >= 2 && arg[0] == '\'' && arg[len(arg)-1] == '\'' {
		arg = arg[1 : len(arg)-1]
	}
	if strings.ContainsAny(arg, "'\"\\ ") {
		return "", "", fmt.Errorf("bad repository path %q", arg)
	}
	arg = strings.TrimPrefix(arg, "~/")
	arg = strings.TrimPrefix(arg, "/")
	arg = strings.TrimSuffix(arg, "/")
	project = strings.TrimSuffix(arg, ".git")
	if err := repo.ValidateName(project); err != nil {
		return "", "", fmt.Errorf("bad repository path %q", arg)
	}
	return service, project, nil
}
