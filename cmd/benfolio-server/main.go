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
	cancelStartup()
	if err != nil {
		return err
	}
	defer store.Close()
	api := httpapi.API{
		Catalog: store, Owner: store.Owner, InstanceID: store.InstanceID,
		SchemaVersion: store.SchemaVersion, BuildVersion: version, SourceConfigured: cfg.BooksDir != "",
		APIKey: cfg.APIKey, Ping: store.Ping, Logger: logger,
	}
	server := &http.Server{
		Addr: cfg.ListenAddress, Handler: api.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 16 * 1024,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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
