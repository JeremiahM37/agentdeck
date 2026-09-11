// Command agentdeck is the control plane: one binary that serves the API, the
// PWA and the scheduler that drives every dispatched agent.
//
// Usage:
//
//	agentdeck            run the control plane
//	agentdeck mcp        speak MCP on stdio against a running control plane
//	agentdeck version    print the version
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/JeremiahM37/agentdeck/internal/app"
	"github.com/JeremiahM37/agentdeck/internal/config"
	"github.com/JeremiahM37/agentdeck/internal/mcp"
	"github.com/JeremiahM37/agentdeck/internal/version"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg := config.Load()
	if len(os.Args) == 1 && interactiveTerminal() {
		if err := clientCommand(cfg, "console", nil); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "console", "tui", "api", "agent", "upload", "files", "download", "skill", "help", "--help", "-h":
			if err := clientCommand(cfg, os.Args[1], os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "serve":
			if len(os.Args) != 2 {
				fmt.Fprintln(os.Stderr, "usage: agentdeck serve")
				os.Exit(2)
			}
		case "attach":
			if err := attach(cfg, os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "mcp":
			// stdio belongs to the protocol here — logs would corrupt the stream
			api := env("AGENTDECK_API", "http://127.0.0.1:"+strconv.Itoa(cfg.Port))
			if err := mcp.New(api, cfg.AuthToken).Serve(os.Stdin, os.Stdout); err != nil {
				os.Exit(1)
			}
			return
		case "version", "--version", "-v":
			fmt.Println(version.Version)
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q (try: --help)\n", os.Args[1])
			os.Exit(2)
		}
	}

	a, err := app.New(cfg, log)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	defer a.Close()

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	srv := &http.Server{
		Addr:    addr,
		Handler: a.Handler(),
		// no WriteTimeout: SSE streams are open-ended by design and any deadline
		// here silently truncates a live timeline
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		log.Info("agentdeck listening", "addr", addr, "version", version.Version,
			"mock", cfg.Mock)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.Server.DrainStreams()
	_ = srv.Shutdown(ctx)
}

// env reads an environment variable with a fallback.
func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
