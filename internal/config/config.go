// Package config loads metagit service settings from flags and environment.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"time"
)

// Config holds settings of the "serve" command.
type Config struct {
	DataDir               string
	HTTPAddr              string
	SSHAddr               string
	BaseURL               string
	SSHURL                string
	TLSCert               string
	TLSKey                string
	PushStatusTimeout     time.Duration
	MaxParallelDeliveries int
	GitTimeout            time.Duration
}

// Default values of the settings.
const (
	DefaultDataDir               = "./data"
	DefaultHTTPAddr              = ":8080"
	DefaultSSHAddr               = ":2222"
	DefaultPushStatusTimeout     = 5 * time.Second
	DefaultMaxParallelDeliveries = 4
	DefaultGitTimeout            = 10 * time.Minute
)

// Load parses serve flags. Precedence: flag, then METAGIT_* environment
// variable, then the default value.
func Load(args []string, getenv func(string) string, output io.Writer) (*Config, error) {
	cfg := &Config{}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(output)
	b := binder{fs: fs, getenv: getenv}

	b.str(&cfg.DataDir, "data-dir", "METAGIT_DATA_DIR", DefaultDataDir, "directory with repositories, database and keys")
	b.str(&cfg.HTTPAddr, "http-addr", "METAGIT_HTTP_ADDR", DefaultHTTPAddr, "address of the HTTP server (web interface, API, git)")
	b.str(&cfg.SSHAddr, "ssh-addr", "METAGIT_SSH_ADDR", DefaultSSHAddr, "address of the SSH server for git")
	b.str(&cfg.BaseURL, "base-url", "METAGIT_BASE_URL", "", "public HTTP(S) URL used to show clone addresses (default derived from --http-addr)")
	b.str(&cfg.SSHURL, "ssh-url", "METAGIT_SSH_URL", "", "public SSH URL used to show clone addresses (default derived from --ssh-addr)")
	b.str(&cfg.TLSCert, "tls-cert", "METAGIT_TLS_CERT", "", "TLS certificate file; without it HTTP is served in plain text")
	b.str(&cfg.TLSKey, "tls-key", "METAGIT_TLS_KEY", "", "TLS key file")
	b.dur(&cfg.PushStatusTimeout, "push-status-timeout", "METAGIT_PUSH_STATUS_TIMEOUT", DefaultPushStatusTimeout, "how long a push waits for delivery results")
	b.int(&cfg.MaxParallelDeliveries, "max-parallel-deliveries", "METAGIT_MAX_PARALLEL_DELIVERIES", DefaultMaxParallelDeliveries, "maximum number of simultaneous deliveries")
	b.dur(&cfg.GitTimeout, "git-timeout", "METAGIT_GIT_TIMEOUT", DefaultGitTimeout, "timeout of a single git operation with a hosting")
	if b.err != nil {
		return nil, b.err
	}
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if err := cfg.finish(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) finish() error {
	if c.DataDir == "" {
		return errors.New("data dir must not be empty")
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return errors.New("--tls-cert and --tls-key must be set together")
	}
	if c.PushStatusTimeout < 0 {
		return errors.New("--push-status-timeout must not be negative")
	}
	if c.MaxParallelDeliveries < 1 {
		return errors.New("--max-parallel-deliveries must be at least 1")
	}
	if c.GitTimeout <= 0 {
		return errors.New("--git-timeout must be positive")
	}
	if c.BaseURL == "" {
		scheme := "http"
		if c.TLSCert != "" {
			scheme = "https"
		}
		c.BaseURL = scheme + "://" + publicHostPort(c.HTTPAddr)
	}
	if c.SSHURL == "" {
		c.SSHURL = "ssh://git@" + publicHostPort(c.SSHAddr)
	}
	return nil
}

// publicHostPort turns a listen address into something a client can use.
func publicHostPort(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return net.JoinHostPort(host, port)
}

// binder registers flags whose defaults come from the environment.
type binder struct {
	fs     *flag.FlagSet
	getenv func(string) string
	err    error
}

func (b *binder) fail(env string, err error) {
	if b.err == nil {
		b.err = fmt.Errorf("invalid %s: %w", env, err)
	}
}

func (b *binder) str(p *string, name, env, def, usage string) {
	if v := b.getenv(env); v != "" {
		def = v
	}
	b.fs.StringVar(p, name, def, usage+" (env "+env+")")
}

func (b *binder) dur(p *time.Duration, name, env string, def time.Duration, usage string) {
	if v := b.getenv(env); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			b.fail(env, err)
		} else {
			def = d
		}
	}
	b.fs.DurationVar(p, name, def, usage+" (env "+env+")")
}

func (b *binder) int(p *int, name, env string, def int, usage string) {
	if v := b.getenv(env); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			b.fail(env, err)
		} else {
			def = n
		}
	}
	b.fs.IntVar(p, name, def, usage+" (env "+env+")")
}

// Names of files and directories inside the data directory.
const (
	DBFile   = "metagit.db"
	ReposDir = "repos"
	HooksDir = "hooks"
	TmpDir   = "tmp"
	BinDir   = "bin"
)

// DBPath returns the database path inside dataDir.
func DBPath(dataDir string) string { return filepath.Join(dataDir, DBFile) }
