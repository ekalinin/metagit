// Package app assembles metagit: storage, git hosting over HTTP and SSH,
// hooks, delivery and the web interface.
package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ekalinin/metagit/internal/api"
	"github.com/ekalinin/metagit/internal/archive"
	"github.com/ekalinin/metagit/internal/auth"
	"github.com/ekalinin/metagit/internal/config"
	"github.com/ekalinin/metagit/internal/core"
	"github.com/ekalinin/metagit/internal/gitcmd"
	"github.com/ekalinin/metagit/internal/githttp"
	"github.com/ekalinin/metagit/internal/gitssh"
	"github.com/ekalinin/metagit/internal/hooks"
	"github.com/ekalinin/metagit/internal/replicate"
	"github.com/ekalinin/metagit/internal/repo"
	"github.com/ekalinin/metagit/internal/secrets"
	"github.com/ekalinin/metagit/internal/store"
	"github.com/ekalinin/metagit/internal/transfer"
	"github.com/ekalinin/metagit/internal/webui"
)

// Files in the data directory.
const (
	HostKeyFile    = "ssh_host_ed25519_key"
	KnownHostsFile = "known_hosts"
)

// Options configure an App.
type Options struct {
	Config *config.Config
	// Binary is the metagit executable called by git hooks and GIT_ASKPASS.
	// The current executable is used when empty.
	Binary string
	// SecretKey is the value of METAGIT_SECRET_KEY.
	SecretKey string
	Log       *slog.Logger
	// WebFS is the built web interface; the embedded one when nil.
	WebFS fs.FS

	// Knobs for tests.
	RetryMin           time.Duration
	RetryMax           time.Duration
	ExtraGitEnv        []string
	TransferWorkFactor int
}

// App is a running metagit instance.
type App struct {
	Config     *config.Config
	Log        *slog.Logger
	Store      *store.Store
	Auth       *auth.Service
	Core       *core.Service
	Repos      *repo.Manager
	Replicator *replicate.Replicator
	Transfer   *transfer.Service

	hooks   *hooks.Server
	sockDir string
	hostKey ssh.Signer
	webFS   fs.FS
}

// New prepares the data directory and starts background parts (hook API,
// archive recovery, delivery workers). Call Serve to accept connections.
func New(ctx context.Context, o Options) (_ *App, err error) {
	cfg := o.Config
	log := cmp.Or(o.Log, slog.Default())
	if _, err := gitcmd.CheckTools(ctx, "git", "ssh"); err != nil {
		return nil, err
	}

	dataDir, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	reposDir := filepath.Join(dataDir, config.ReposDir)
	hooksDir := filepath.Join(dataDir, config.HooksDir)
	tmpDir := filepath.Join(dataDir, config.TmpDir)
	binDir := filepath.Join(dataDir, config.BinDir)
	for _, d := range []string{dataDir, reposDir, hooksDir, tmpDir, binDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	// Leftovers of a crash: temporary key files and request bodies.
	if entries, err := os.ReadDir(tmpDir); err == nil {
		for _, e := range entries {
			_ = os.RemoveAll(filepath.Join(tmpDir, e.Name()))
		}
	}

	binary := o.Binary
	if binary == "" {
		if binary, err = os.Executable(); err != nil {
			return nil, err
		}
	}
	if resolved, err := filepath.EvalSymlinks(binary); err == nil {
		binary = resolved
	}
	if err := repo.WriteHooks(hooksDir, binary); err != nil {
		return nil, err
	}
	askpass := filepath.Join(binDir, "askpass")
	if err := repo.WriteScript(askpass, binary, "askpass"); err != nil {
		return nil, err
	}

	a := &App{Config: cfg, Log: log, webFS: o.WebFS}
	if a.webFS == nil {
		a.webFS = webui.Embedded()
	}
	defer func() {
		if err != nil {
			a.Close()
		}
	}()

	if a.Store, err = store.Open(ctx, config.DBPath(dataDir)); err != nil {
		return nil, err
	}
	key, err := secrets.LoadKey(dataDir, o.SecretKey)
	if err != nil {
		return nil, err
	}
	box, err := secrets.NewBox(key)
	if err != nil {
		return nil, err
	}
	a.Repos = &repo.Manager{ReposDir: reposDir, HooksDir: hooksDir, Git: &gitcmd.Runner{Timeout: time.Minute}}
	if err := a.configureRepos(ctx); err != nil {
		return nil, err
	}
	arch := &archive.Service{Store: a.Store, Repos: a.Repos, Git: &gitcmd.Runner{Timeout: time.Minute}, Log: log}
	a.Replicator = replicate.New(a.Store, a.Repos, box, arch, replicate.Options{
		MaxParallel: cfg.MaxParallelDeliveries,
		GitTimeout:  cfg.GitTimeout,
		RetryMin:    o.RetryMin,
		RetryMax:    o.RetryMax,
		Askpass:     askpass,
		TmpDir:      tmpDir,
		KnownHosts:  filepath.Join(dataDir, KnownHostsFile),
		ExtraEnv:    o.ExtraGitEnv,
	}, log)
	a.Core = &core.Service{
		Store:             a.Store,
		Repos:             a.Repos,
		Box:               box,
		Archive:           arch,
		Replicator:        a.Replicator,
		Log:               log,
		PushStatusTimeout: cfg.PushStatusTimeout,
	}
	a.Auth = auth.New(a.Store)
	a.Transfer = &transfer.Service{Store: a.Store, Repos: a.Repos, Box: box, WorkFactor: o.TransferWorkFactor}
	if a.hostKey, err = gitssh.LoadOrCreateHostKey(filepath.Join(dataDir, HostKeyFile)); err != nil {
		return nil, err
	}

	// Unix socket paths are short (about 100 bytes), so the socket lives in
	// a private temporary directory rather than in the data directory.
	if a.sockDir, err = os.MkdirTemp("", "metagit-"); err != nil {
		return nil, err
	}
	if a.hooks, err = hooks.Listen(filepath.Join(a.sockDir, "hooks.sock"), a.Core, log); err != nil {
		return nil, err
	}
	if err := arch.Recover(ctx); err != nil {
		log.Error("archive recovery failed", "err", err)
	}
	if err := a.Replicator.Start(ctx); err != nil {
		return nil, err
	}
	if _, exists, err := a.Auth.Owner(ctx); err == nil && !exists {
		log.Warn("owner is not created: run `metagit owner create --name <name> --data-dir " + cfg.DataDir + "`")
	}
	return a, nil
}

// configureRepos re-applies repository settings: the hooks path changes when
// the data directory is moved.
func (a *App) configureRepos(ctx context.Context) error {
	projects, err := a.Store.ListProjects(ctx)
	if err != nil {
		return err
	}
	for _, p := range projects {
		if !a.Repos.Exists(p.Name) {
			a.Log.Error("repository of a project is missing", "project", p.Name, "path", a.Repos.Path(p.Name))
			continue
		}
		if err := a.Repos.Configure(ctx, p.Name); err != nil {
			return fmt.Errorf("configure repository %s: %w", p.Name, err)
		}
	}
	return nil
}

// Handler routes /api/ to the API, /<project>.git/ to git and the rest to the
// web interface.
func (a *App) Handler() http.Handler {
	apiHandler := (&api.API{
		Core:          a.Core,
		Auth:          a.Auth,
		Transfer:      a.Transfer,
		BaseURL:       a.Config.BaseURL,
		SSHURL:        a.Config.SSHURL,
		SecureCookies: a.Config.TLSCert != "" || strings.HasPrefix(a.Config.BaseURL, "https://"),
		Log:           a.Log,
	}).Handler()
	gitHandler := &githttp.Handler{
		ReposDir:      a.Repos.ReposDir,
		CheckToken:    a.Auth.CheckToken,
		ProjectExists: a.Core.ProjectExists,
		Owner: func(ctx context.Context) string {
			name, _, _ := a.Auth.Owner(ctx)
			return cmp.Or(name, "owner")
		},
		Pushes: a.hooks,
		Log:    a.Log,
		TmpDir: filepath.Join(filepath.Dir(a.Repos.ReposDir), config.TmpDir),
	}
	spa := webui.Handler(a.webFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/api" || strings.HasPrefix(path, "/api/"):
			apiHandler.ServeHTTP(w, r)
		case isGitPath(path):
			gitHandler.ServeHTTP(w, r)
		default:
			spa.ServeHTTP(w, r)
		}
	})
}

func isGitPath(path string) bool {
	_, ok := githttp.ProjectFromPath(path)
	return ok
}

// Serve accepts HTTP and SSH connections until ctx is done, then shuts the
// servers down gracefully.
func (a *App) Serve(ctx context.Context, httpL, sshL net.Listener) error {
	httpSrv := &http.Server{
		Handler:           a.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(a.Log.Handler(), slog.LevelWarn),
	}
	sshSrv := &gitssh.Server{
		ReposDir:      a.Repos.ReposDir,
		HostKey:       a.hostKey,
		CheckKey:      a.Auth.CheckSSHKey,
		ProjectExists: a.Core.ProjectExists,
		Pushes:        a.hooks,
		Log:           a.Log,
	}
	errc := make(chan error, 2)
	go func() {
		var err error
		if a.Config.TLSCert != "" {
			err = httpSrv.ServeTLS(httpL, a.Config.TLSCert, a.Config.TLSKey)
		} else {
			err = httpSrv.Serve(httpL)
		}
		if !errors.Is(err, http.ErrServerClosed) {
			errc <- fmt.Errorf("http server: %w", err)
		}
	}()
	go func() {
		if err := sshSrv.Serve(sshL); err != nil {
			errc <- fmt.Errorf("ssh server: %w", err)
		}
	}()

	var err error
	select {
	case <-ctx.Done():
	case err = <-errc:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	_ = sshSrv.Close()
	return err
}

// Close stops background work and releases resources.
func (a *App) Close() {
	if a.Replicator != nil {
		a.Replicator.Stop()
	}
	if a.hooks != nil {
		_ = a.hooks.Close()
	}
	if a.sockDir != "" {
		_ = os.RemoveAll(a.sockDir)
	}
	if a.Store != nil {
		_ = a.Store.Close()
	}
}
