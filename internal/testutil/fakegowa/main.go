package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	mode := env("FAKE_GOWA_MODE", "serve")
	if mode == "crash" {
		os.Exit(envInt("FAKE_GOWA_EXIT_CODE", 1))
	}

	if pidFile := os.Getenv("FAKE_GOWA_PID_FILE"); pidFile != "" {
		_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600)
	}

	var child *exec.Cmd
	if mode == "spawn-child" {
		var err error
		child, err = spawnChild()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer cleanupChild(child)
	}

	if mode == "ignore-term" {
		signal.Ignore(os.Interrupt, syscall.SIGTERM)
	} else {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-stop
			cleanupChild(child)
			os.Exit(0)
		}()
	}

	if mode == "delayed-ready" {
		time.Sleep(time.Duration(envInt("FAKE_GOWA_READY_DELAY_MS", 0)) * time.Millisecond)
	}

	if mode == "load" {
		startLoad()
	}

	port, err := parsePort(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	server := &http.Server{Addr: "127.0.0.1:" + strconv.Itoa(port), Handler: mux}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parsePort(args []string) (int, error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--port=") {
			return strconv.Atoi(strings.TrimPrefix(arg, "--port="))
		}
		if arg == "--port" && i+1 < len(args) {
			return strconv.Atoi(args[i+1])
		}
	}
	return 0, fmt.Errorf("missing --port")
}

func spawnChild() (*exec.Cmd, error) {
	cmd := exec.Command(os.Args[0], "--fake-gowa-child")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawn child: %w", err)
	}
	if childPIDFile := os.Getenv("FAKE_GOWA_CHILD_PID_FILE"); childPIDFile != "" {
		if err := os.WriteFile(childPIDFile, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o600); err != nil {
			cleanupChild(cmd)
			return nil, fmt.Errorf("write child pid file: %w", err)
		}
	}
	return cmd, nil
}

func cleanupChild(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func startLoad() {
	loadBytes := envInt("FAKE_GOWA_LOAD_BYTES", 1024*1024)
	buf := make([]byte, loadBytes)
	for i := range buf {
		buf[i] = byte(i)
	}
	go func() {
		var n uint64
		for {
			n++
			if n%10_000_000 == 0 {
				runtime.Gosched()
			}
		}
	}()
	go func() {
		for {
			if len(buf) > 0 {
				buf[0]++
			}
			time.Sleep(time.Second)
		}
	}()
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func init() {
	if len(os.Args) == 2 && os.Args[1] == "--fake-gowa-child" {
		select {}
	}
}
