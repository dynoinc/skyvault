package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"connectrpc.com/connect"
	batcherv1connect "github.com/dynoinc/skyvault/gen/proto/batcher/v1/v1connect"
	cachev1connect "github.com/dynoinc/skyvault/gen/proto/cache/v1/v1connect"
	"github.com/dynoinc/skyvault/internal/batcher"
	"github.com/dynoinc/skyvault/internal/cache"
	"github.com/dynoinc/skyvault/internal/database"
	"github.com/dynoinc/skyvault/internal/middleware"
	"github.com/dynoinc/skyvault/internal/storage"
	"github.com/earthboundkid/versioninfo/v2"
	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	"github.com/lmittmann/tint"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type config struct {
	Addr        string `split_words:"true" default:"127.0.0.1:5001"`
	DatabaseURL string `split_words:"true" default:"postgres://postgres:postgres@127.0.0.1:5431/postgres?sslmode=disable"`
	StorageURL  string `split_words:"true" default:"filesystem://objstore"`

	Batcher batcher.Config
	Cache   cache.Config
}

func main() {
	help := flag.Bool("help", false, "Show help")
	version := flag.Bool("version", false, "Show version")
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	if *help {
		_ = envconfig.Usage("skyvault", &config{})
		return
	}

	if *version {
		fmt.Println(versioninfo.Short())
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		slog.ErrorContext(ctx, "error loading .env file", "error", err)
		os.Exit(1)
	}

	var c config
	if err := envconfig.Process("skyvault", &c); err != nil {
		slog.ErrorContext(ctx, "error processing environment variables", "error", err)
		os.Exit(1)
	}

	// Logging setup
	shortfile := func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == slog.SourceKey {
			s := a.Value.Any().(*slog.Source)
			s.File = path.Base(s.File)
		}
		return a
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource:   true,
		ReplaceAttr: shortfile,
	}))
	if *debug {
		logger = slog.New(tint.NewHandler(os.Stderr, &tint.Options{
			AddSource:   true,
			Level:       slog.LevelDebug,
			TimeFormat:  time.Kitchen,
			ReplaceAttr: shortfile,
		}))
	}
	slog.SetDefault(logger)
	slog.InfoContext(ctx, "Starting skyvault", "version", versioninfo.Short())

	// Metrics setup
	promExporter, err := prometheus.New()
	if err != nil {
		slog.ErrorContext(ctx, "setting up Prometheus exporter", "error", err)
		os.Exit(1)
	}
	meterProvider := metric.NewMeterProvider(metric.WithReader(promExporter))
	otel.SetMeterProvider(meterProvider)

	// Database setup
	db, err := database.Pool(ctx, c.DatabaseURL)
	if err != nil {
		slog.ErrorContext(ctx, "setting up database", "error", err)
		os.Exit(1)
	}

	// Storage setup
	store, err := storage.New(ctx, c.StorageURL)
	if err != nil {
		slog.ErrorContext(ctx, "setting up storage", "error", err)
		os.Exit(1)
	}

	// Set up HTTP mux
	mux := http.NewServeMux()

	// Common interceptors for all services
	interceptors := connect.WithInterceptors(
		middleware.LogErrors(),
		// Add any other global interceptors here
	)

	// Register Connect service handlers
	if c.Batcher.Enabled {
		batcherHandler := batcher.NewHandler(ctx, c.Batcher, database.New(db), store)
		path, handler := batcherv1connect.NewBatcherServiceHandler(
			batcherHandler,
			interceptors,
		)
		mux.Handle(path, handler)
	}

	if c.Cache.Enabled {
		cacheHandler := cache.NewHandler(ctx, c.Cache, store)
		path, handler := cachev1connect.NewCacheServiceHandler(
			cacheHandler,
			interceptors,
		)
		mux.Handle(path, handler)
	}

	// Register HTTP endpoints
	mux.HandleFunc("GET /metrics", promhttp.Handler().ServeHTTP)
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fmt.Sprintf(`{"version": "%s"}`, versioninfo.Short())))
	})

	// Create server with h2c support for unencrypted HTTP/2
	server := &http.Server{
		Addr:    c.Addr,
		Handler: h2c.NewHandler(mux, &http2.Server{}),
	}

	// Start server in a goroutine
	go func() {
		slog.InfoContext(ctx, "starting server", "addr", c.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.ErrorContext(ctx, "server error", "error", err)
			os.Exit(1)
		}
	}()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Block until we receive a signal
	sig := <-sigChan
	slog.InfoContext(ctx, "received shutdown signal", "signal", sig)

	// Cancel context to notify dependents
	cancel()

	// Create a timeout context for graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Graceful shutdown
	slog.InfoContext(ctx, "shutting down server")
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.WarnContext(ctx, "server shutdown failed", "error", err)
	}

	slog.InfoContext(ctx, "shutdown complete")
}
