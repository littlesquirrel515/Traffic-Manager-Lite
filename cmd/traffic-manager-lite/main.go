package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"
	"traffic-manager-lite/internal/collector"
	"traffic-manager-lite/internal/config"
	"traffic-manager-lite/internal/httpapi"
	"traffic-manager-lite/internal/security"
	"traffic-manager-lite/internal/storage"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:8080/api/v1/health", nil)
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			os.Exit(1)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	if e := run(); e != nil {
		slog.Error("service stopped", "error", e)
		os.Exit(1)
	}
}
func run() error {
	c, e := config.Load()
	if e != nil {
		return e
	}
	level := slog.LevelInfo
	if e = level.UnmarshalText([]byte(c.LogLevel)); e != nil {
		return fmt.Errorf("invalid TML_LOG_LEVEL")
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
	store, e := storage.Open(c.DBPath, c.Timezone)
	if e != nil {
		return e
	}
	defer store.Close()
	var saved string
	if e = store.DB.QueryRow("SELECT value FROM settings WHERE key='runtime'").Scan(&saved); e == nil {
		var d struct {
			Interval string `json:"collect_interval"`
			Window   string `json:"active_window"`
			LogLevel string `json:"log_level"`
			Archive  bool   `json:"archive_enabled"`
		}
		if e = json.Unmarshal([]byte(saved), &d); e != nil {
			return fmt.Errorf("invalid saved settings")
		}
		c.Interval, e = time.ParseDuration(d.Interval)
		if e != nil || c.Interval < time.Second {
			return fmt.Errorf("invalid saved collection interval")
		}
		c.ActiveWindow, e = time.ParseDuration(d.Window)
		if e != nil || c.ActiveWindow < time.Second {
			return fmt.Errorf("invalid saved active window")
		}
		c.LogLevel = d.LogLevel
		c.Archive = d.Archive
		level.UnmarshalText([]byte(c.LogLevel))
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
	}
	auth, e := security.NewAuth(c.AdminUser, c.AdminPassword, c.SecureCookie)
	if e != nil {
		return e
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	scheduler := collector.New(ctx, store, c)
	api := &httpapi.API{Store: store, Scheduler: scheduler, Config: c, Auth: auth}
	server := &http.Server{Addr: c.Listen, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 16}
	done := make(chan struct{})
	go func() { defer close(done); scheduler.Run() }()
	errors := make(chan error, 1)
	go func() { slog.Info("service listening", "address", c.Listen); errors <- server.ListenAndServe() }()
	select {
	case e := <-errors:
		cancel()
		<-done
		if e != http.ErrServerClosed {
			return e
		}
	case <-ctx.Done():
		shutdown, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		e = server.Shutdown(shutdown)
		if e != nil {
			server.Close()
		}
		<-done
		return e
	}
	return nil
}
