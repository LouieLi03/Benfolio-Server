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
	"syscall"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/config"
	"github.com/LouieLi03/Benfolio-Server/internal/httpapi"
	"github.com/LouieLi03/Benfolio-Server/internal/importer"
	"github.com/LouieLi03/Benfolio-Server/internal/originals"
	"github.com/LouieLi03/Benfolio-Server/internal/scan"
	"github.com/LouieLi03/Benfolio-Server/internal/source"
	"github.com/LouieLi03/Benfolio-Server/internal/storage"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	var err error
	switch {
	case len(os.Args) == 1:
		err = run(logger)
	case len(os.Args) == 2 && os.Args[1] == "healthcheck":
		err = healthcheck()
	case len(os.Args) == 2 && os.Args[1] == "version":
		fmt.Println(version)
	default:
		err = errors.New("用法: benfolio-server [healthcheck|version]")
	}
	if err != nil {
		logger.Error("server_stopped", "error", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	layout, err := storage.OpenLayout(cfg.DataDir)
	if err != nil {
		return err
	}
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 30*time.Second)
	store, err := storage.Open(startupCtx, layout.Database)
	if err != nil {
		cancelStartup()
		return err
	}
	defer store.Close()
	if cfg.BooksDir != "" {
		err = store.ConfigureLibrary(startupCtx, cfg.LibraryName, cfg.BooksDir)
	}
	cancelStartup()
	if err != nil {
		return fmt.Errorf("登记书库来源: %w", err)
	}
	sources, err := source.NewBrowser(store, store.SourceLibraryID, cfg.BooksDir)
	if err != nil {
		return err
	}
	defer sources.Close()
	files, err := originals.New(store, layout.Originals)
	if err != nil {
		return err
	}
	defer files.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	recoveryCtx, cancelRecovery := context.WithTimeout(ctx, 30*time.Second)
	err = store.RecoverScans(recoveryCtx, store.Owner.ID)
	cancelRecovery()
	if err != nil {
		return fmt.Errorf("恢复扫描任务状态: %w", err)
	}
	workerCtx, cancelWorker := context.WithCancel(ctx)
	defer cancelWorker()
	imports := importer.New(store, sources, layout.Staging, layout.Originals)
	scans := scan.NewRunner(workerCtx, store, sources, imports, store.Owner.ID, logger)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		scans.Run()
	}()
	defer func() {
		cancelWorker()
		<-workerDone
	}()
	api := httpapi.API{
		Catalog:          store,
		Sources:          sources,
		Scans:            scans,
		Imports:          imports,
		Originals:        files,
		Owner:            store.Owner,
		InstanceID:       store.InstanceID,
		ServerName:       cfg.ServerName,
		SchemaVersion:    store.SchemaVersion,
		BuildVersion:     version,
		SourceConfigured: store.SourceLibraryID != "",
		APIKey:           cfg.APIKey,
		Ping:             store.Ping,
		Logger:           logger,
	}
	server := &http.Server{
		Addr: cfg.ListenAddress, Handler: api.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 16 * 1024,
		BaseContext:    func(net.Listener) context.Context { return ctx },
	}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	logger.Info("server_starting", "address", cfg.ListenAddress, "instance_id", store.InstanceID, "version", version)
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("停止服务: %w", err)
		}
		logger.Info("server_stopped")
		return nil
	}
}

// The container probe uses only the public database-readiness endpoint and does
// not load credentials, scan the book source, or trigger a database migration.
func healthcheck() error {
	host, port, err := net.SplitHostPort(config.ListenAddress())
	if err != nil {
		return errors.New("无效的监听地址")
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err := client.Get("http://" + net.JoinHostPort(host, port) + "/health/ready")
	if err != nil {
		return errors.New("服务尚未就绪")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("服务尚未就绪")
	}
	return nil
}
