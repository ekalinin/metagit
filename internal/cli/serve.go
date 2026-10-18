package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"

	"github.com/ekalinin/metagit/internal/app"
	"github.com/ekalinin/metagit/internal/config"
)

func runServe(ctx context.Context, args []string, env Env) int {
	cfg, err := config.Load(args, env.Getenv, env.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(env.Stderr, "error:", err)
		return 2
	}
	log := slog.New(slog.NewTextHandler(env.Stderr, nil))
	a, err := app.New(ctx, app.Options{Config: cfg, SecretKey: env.Getenv("METAGIT_SECRET_KEY"), Log: log})
	if err != nil {
		log.Error("cannot start", "err", err)
		return 1
	}
	defer a.Close()

	httpL, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		log.Error("cannot listen", "addr", cfg.HTTPAddr, "err", err)
		return 1
	}
	sshL, err := net.Listen("tcp", cfg.SSHAddr)
	if err != nil {
		httpL.Close()
		log.Error("cannot listen", "addr", cfg.SSHAddr, "err", err)
		return 1
	}
	log.Info("metagit started", "version", versionString(), "http", httpL.Addr().String(), "ssh", sshL.Addr().String(),
		"base_url", cfg.BaseURL, "ssh_url", cfg.SSHURL, "data_dir", cfg.DataDir)
	if err := a.Serve(ctx, httpL, sshL); err != nil {
		log.Error("server stopped", "err", err)
		return 1
	}
	log.Info("metagit stopped")
	return 0
}
