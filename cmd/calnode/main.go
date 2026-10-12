package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // embed IANA timezone database so scratch/distroless images work

	"github.com/calnode/calnode/internal/buildinfo"
	"github.com/calnode/calnode/internal/config"
	"github.com/calnode/calnode/internal/db"
	"github.com/calnode/calnode/internal/keyvault"
	"github.com/calnode/calnode/internal/server"
)

func main() { runCLI(os.Args[1:]) }

func runServer(cfg *config.Config) {

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(logger)

	bi := buildinfo.Get()
	logger.Info("starting openslot", "version", bi.Version, "commit", bi.Commit, "build_time", bi.BuildTime, "dirty", bi.Dirty)

	// Config whose wrong value is worse than its absence is checked here rather than
	// tolerated at request time — see (*config.Config).Validate.
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(cfg.BindHost, cfg.Port))
	if err != nil {
		logger.Error("cannot listen; check the port and bind address", "error", err)
		os.Exit(1)
	}
	defer listener.Close()

	database, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
	logger.Info("database migrations applied")

	// Open the key vault. devMode allows an ephemeral DEK when no secret is set
	// (handy for local development); production deployments must set
	// CALNODE_ENCRYPTION_KEY or the vault will refuse to start.
	devMode := !strings.HasPrefix(cfg.BaseURL, "https://")
	vault, err := keyvault.Open(database, cfg.EncryptionKey, cfg.RecoverySecret, devMode)
	if err != nil {
		logger.Error("keyvault: failed to open", "error", err)
		os.Exit(1)
	}
	// Replace the platform secret in cfg with the real DEK so all downstream
	// components (handler, gcal, webhook) receive the AES key they expect.
	cfg.EncryptionKey = vault.DEKHex()

	workerCtx, workerCancel := context.WithCancel(context.Background())

	srv, drainWorker := server.New(workerCtx, cfg, database, logger)

	httpServer := &http.Server{
		Addr:         net.JoinHostPort(cfg.BindHost, cfg.Port),
		Handler:      srv,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)
	serveErr := make(chan error, 1)

	go func() {
		logger.Info("server listening", "port", cfg.Port, "base_url", cfg.BaseURL)
		logger.Info("open your scheduling workspace", "url", strings.TrimRight(cfg.BaseURL, "/")+"/admin/")
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			serveErr <- err
		}
	}()

	select {
	case <-quit:
	case err := <-serveErr:
		workerCancel()
		logger.Error("server stopped unexpectedly", "error", err)
		os.Exit(1)
	}
	logger.Info("shutting down — draining worker and in-flight requests")

	// Stop the worker's ticker loop and drain the HTTP server concurrently so a
	// slow poll cycle (up to 10 jobs × 10 s each) does not delay in-flight HTTP
	// request draining. Both must complete before the process exits.
	workerCancel()

	workerDone := make(chan struct{})
	go func() {
		drainWorker()
		close(workerDone)
	}()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("forced shutdown", "error", err)
		os.Exit(1)
	}

	// Bound the worker drain: Poll uses context.Background internally, so an
	// in-flight cycle (up to 10 jobs × 30 s SMTP bounds) could otherwise linger
	// for minutes on SIGTERM and stall deploys. Jobs are idempotent across
	// restarts (claimed rows, retried payloads), so abandoning the drain is safe.
	select {
	case <-workerDone: // wait for current poll cycle to complete before db.Close()
	case <-time.After(60 * time.Second):
		logger.Error("worker drain timed out; exiting with jobs to retry on restart")
	}
	logger.Info("server stopped")
}
