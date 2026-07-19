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

	if err := app.Run(ctx, app.Options{
		Config:        cfg,
		Logger:        slog.New(slog.NewTextHandler(stderr, nil)),
		ForceShutdown: force,
	}); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return 0
}
