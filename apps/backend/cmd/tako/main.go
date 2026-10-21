package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/velopulent/tako/internal/app"
	"github.com/velopulent/tako/internal/bridge"
	"github.com/velopulent/tako/internal/config"
	"github.com/velopulent/tako/internal/distro"
	"github.com/velopulent/tako/internal/fdpass"
	"github.com/velopulent/tako/internal/logging"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/sessiond"
	"go.uber.org/zap"
)

func init() {
	// The sign-in helper runs PAM session modules that only work on the main
	// thread: pam_loginuid writes /proc/self/loginuid, which the kernel
	// allows only for the thread group leader. Locking in init is the only
	// way to keep main.main on the main thread.
	if len(os.Args) > 1 && os.Args[1] == "session-helper" {
		runtime.LockOSThread()
	}
}

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
	updates := distro.NewUpdateService()
	switch os.Args[1] {
	case "serve":
		err = serve(os.Args[2:])
	case "sessiond":
		executable, executableErr := os.Executable()
		if executableErr != nil {
			err = executableErr
			break
		}
		updates.UseWorker(executable)
		err = sessiond.Run(os.Args[2:], updates)
	case "bridge":
		err = runBridge(os.Args[2:])
	case "session-helper":
		err = sessiond.RunLoginHelper()
	case "update-worker":
		err = runUpdateWorker(os.Args[2:], updates)
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
	reload := make(chan os.Signal, 1)
	signal.Notify(reload, syscall.SIGHUP)
	defer signal.Stop(reload)
	go func() {
		for range reload {
			server.ReloadCertificate()
		}
	}()
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
	case <-server.Idle():
		zap.L().Info("gateway idle; shutting down")
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

internal modes, started by sessiond:
  tako session-helper                  one sign-in's PAM session
  tako bridge [--privileged]           per-user (or administrative) framed RPC bridge
  tako update-worker --job-file path   one software update transaction`)
}

// runBridge serves the bridge on stdin and stdout. Sessiond's sign-in helper
// sets TAKO_TERMINAL_FD for a user's bridge so it can host terminals.
func runBridge(args []string) error {
	flags := flag.NewFlagSet("bridge", flag.ContinueOnError)
	privileged := flags.Bool("privileged", false, "serve administrative work as root")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *privileged {
		return bridge.RunPrivileged(os.Stdin, os.Stdout, os.Stderr)
	}
	value := os.Getenv("TAKO_TERMINAL_FD")
	if value == "" {
		return bridge.Run(os.Stdin, os.Stdout, os.Stderr)
	}
	descriptor, err := strconv.Atoi(value)
	if err != nil || descriptor < 3 {
		return fmt.Errorf("invalid TAKO_TERMINAL_FD %q", value)
	}
	terminals, err := fdpass.Conn(os.NewFile(uintptr(descriptor), "terminals"))
	if err != nil {
		return fmt.Errorf("terminal socket: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	defer stop()
	return bridge.RunSession(ctx, os.Stdin, os.Stdout, os.Stderr, terminals)
}

func runUpdateWorker(args []string, updates *platform.UpdateService) error {
	flags := flag.NewFlagSet("update-worker", flag.ContinueOnError)
	jobFile := flags.String("job-file", "", "root-owned update job file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	operation, err := platform.ReadUpdateWorkerJob(*jobFile)
	if err != nil {
		return err
	}
	// Progress goes to sessiond over a pipe. If sessiond stops mid-transaction
	// the pipe breaks; ignoring SIGPIPE turns that into ignored write errors so
	// the package manager is never killed halfway through.
	signal.Ignore(syscall.SIGPIPE)
	return updates.RunWorker(context.Background(), operation, os.Stdout)
}
