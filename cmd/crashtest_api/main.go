package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"crashtest_api/internal/api"
	"crashtest_api/internal/config"
	"crashtest_api/internal/db"
	"crashtest_api/internal/migrations"
	"crashtest_api/internal/proxy"
)

func withCORS(allowed map[string]struct{}, next http.Handler) http.Handler {
	allowAny := len(allowed) == 0
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if _, ok := allowed[origin]; ok || allowAny {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Max-Age", "3600")
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func parseAllowedOrigins(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, o := range strings.Split(raw, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			out[o] = struct{}{}
		}
	}
	return out
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := db.Initialize(ctx, cfg.DatabaseURL, migrations.FS); err != nil {
		logger.Error("initialize db", "error", err)
		os.Exit(1)
	}

	store, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("open db", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	mux := http.NewServeMux()
	mux.Handle("/", proxy.New(store, logger))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	api.New(store, logger).Register(mux)

	allowed := parseAllowedOrigins(os.Getenv("ALLOWED_ORIGINS"))
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           withCORS(allowed, mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("listening", "addr", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "error", err)
	}
}
