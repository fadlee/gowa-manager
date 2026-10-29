package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/fadlee/gowa-manager/internal/app"
	"github.com/fadlee/gowa-manager/internal/buildinfo"
	"github.com/fadlee/gowa-manager/internal/config"
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	cfg, action, err := config.Parse(args, getenv)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	switch action {
	case config.ActionHelp:
		fmt.Fprint(stdout, config.HelpText())
		return 0
	case config.ActionVersion:
		fmt.Fprintln(stdout, config.VersionText())
		return 0
	case config.ActionResetPassword:
		password, err := app.ResetPassword(context.Background(), cfg.DataDir, cfg.ResetUsername, cfg.ResetPassword)
		if err != nil {
			fmt.Fprintln(stderr, "❌ "+err.Error())
			return 1
		}
		fmt.Fprintf(stdout, "✅ Password for %q reset. All of the user's sessions were signed out.\n", cfg.ResetUsername)
		if cfg.ResetPassword == "" {
			fmt.Fprintf(stdout, "New password: %s\n", password)
		}
		return 0
	}

	// Signal handling: the first SIGINT/SIGTERM initiates graceful shutdown
	// (cancels the context). A second SIGINT/SIGTERM forces immediate
	// shutdown by closing the force channel, which makes Run skip the
	// graceful drain.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	force := make(chan struct{})

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		<-sigCh // first signal: graceful shutdown
		cancel()
		<-sigCh // second signal: force
		close(force)
	}()

	// In verbose mode, log at INFO level (detailed slog output). In the
	// default (quiet) mode, log at WARN level so only warnings/errors
	// reach stderr; a user-friendly banner is printed via OnStarted.
	level := slog.LevelWarn
	if cfg.Verbose {
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))

	var setupToken string
	onStarted := func(addr string) {
		fmt.Fprint(stdout, startupBanner(addr, setupToken))
	}

	if err := app.Run(ctx, app.Options{
		Config:        cfg,
		Logger:        logger,
		ForceShutdown: force,
		OnStarted:     onStarted,
		OnSetupToken:  func(token string) { setupToken = token },
	}); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return 0
}

// startupBanner returns the user-friendly message printed to stdout when
// the HTTP server starts listening. When no users exist yet, it includes
// the one-time setup token needed to register the first superadmin.
func startupBanner(addr, setupToken string) string {
	setup := ""
	if setupToken != "" {
		setup = fmt.Sprintf("  Setup:    No users yet. Open the Web UI to register the\n            first superadmin with this setup token:\n            %s\n", setupToken)
	}
	return fmt.Sprintf(`==================================================
  %s
==================================================
  Web UI:   http://%s
%s  Status:   Ready! Press Ctrl+C to stop.
==================================================
`, buildinfo.DisplayVersion(), addr, setup)
}
