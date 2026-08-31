package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/velopulent/tako/internal/app"
	"github.com/velopulent/tako/internal/bridge"
	"github.com/velopulent/tako/internal/config"
	"github.com/velopulent/tako/internal/logging"
	"github.com/velopulent/tako/internal/sessiond"
	"go.uber.org/zap"
)

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) < 2 {
		usage()
		return 2
	}
	logger, err := logging.New(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer func() { _ = logger.Sync() }()
	zap.ReplaceGlobals(logger)
	logger.Info("process starting")

	err = nil
	switch os.Args[1] {
	case "serve":
		err = serve(os.Args[2:])
	case "sessiond":
		err = sessiond.Run(os.Args[2:])
	case "bridge":
		err = bridge.Run(os.Stdin, os.Stdout, os.Stderr)
	case "help", "--help", "-h":
		usage()
		return 0
	default:
		usage()
		return 2
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		logger.Error("process stopped", zap.Error(err))
		return 1
	}
	logger.Info("process stopped")
	return 0
}

func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/tako/config.toml", "configuration file")
	dev := flags.Bool("dev", false, "serve loopback HTTP with trusted local development login")
	if err := flags.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath, *dev)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	server, err := app.New(cfg)
	if err != nil {
		return fmt.Errorf("initialize server: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	zap.L().Info("gateway configured",
		zap.String("address", cfg.Address),
		zap.Bool("development", cfg.Development),
		zap.String("session_socket", cfg.SessionSocket),
	)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  tako serve [--config path] [--dev]  unprivileged HTTPS gateway
  tako sessiond [--socket path] [--config path] privileged PAM/session boundary
  tako bridge                          per-user framed RPC bridge`)
}
