package main

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TheOutdoorProgrammer/crate/internal/activity"
	"github.com/TheOutdoorProgrammer/crate/internal/api"
	"github.com/TheOutdoorProgrammer/crate/internal/cache"
	"github.com/TheOutdoorProgrammer/crate/internal/config"
	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/provider"
	"github.com/TheOutdoorProgrammer/crate/internal/services/downloader"
	"github.com/TheOutdoorProgrammer/crate/internal/services/navidrome"
	"github.com/TheOutdoorProgrammer/crate/internal/services/organizer"
	"github.com/TheOutdoorProgrammer/crate/internal/services/scheduler"
	"github.com/TheOutdoorProgrammer/crate/internal/services/slskd"
)

var Version = "dev"

//go:embed all:dist
var frontendDist embed.FS

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg := config.Load()

	database, err := db.Open(cfg.DatabasePath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	providerCache, err := cache.Open(cfg.CachePath)
	if err != nil {
		slog.Error("failed to open cache", "error", err)
		os.Exit(1)
	}
	defer providerCache.Close()

	frontendFS, _ := fs.Sub(frontendDist, "dist")

	actLog, err := activity.NewLog(cfg.ActivityPath)
	if err != nil {
		slog.Error("failed to open activity log", "error", err)
		os.Exit(1)
	}
	defer actLog.Close()

	queries := db.NewQueries(database)
	providerMgr := provider.NewManager(providerCache, queries)
	slskdClient := slskd.NewClient(cfg.SlskdURL, cfg.SlskdAPIKey)
	org := organizer.NewService(queries, cfg.DownloadsDir, cfg.LibraryPath)
	dl := downloader.NewService(queries, slskdClient, org, actLog)
	dl.AddNotifier(navidrome.NewClient(queries))
	server := api.NewServer(queries, providerMgr, providerCache, dl, actLog, frontendFS, cfg.LibraryPath, Version)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	procMgr := provider.NewProcessManager()
	configs := provider.ParseProviderConfig(cfg.Providers)
	if err := procMgr.StartProviders(ctx, providerMgr, configs); err != nil {
		slog.Error("failed to start providers", "error", err)
		os.Exit(1)
	}

	go dl.Run(ctx, 10*time.Second)

	sched := scheduler.NewService(queries, providerMgr, actLog, cfg.LibraryPath, cfg.ScanInterval)
	go sched.Run(ctx)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      server,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("crate starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-quit:
	case err := <-errCh:
		slog.Error("server error", "error", err)
	}

	slog.Info("shutting down")
	cancel()
	procMgr.StopAll()
	providerMgr.Close()
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	srv.Shutdown(shutCtx)
}
