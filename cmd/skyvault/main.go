package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	batcherv1 "github.com/dynoinc/skyvault/gen/proto/batcher/v1"
	"github.com/dynoinc/skyvault/internal/batcher"
	"github.com/dynoinc/skyvault/internal/database"
	"github.com/dynoinc/skyvault/internal/storage"
	"github.com/earthboundkid/versioninfo/v2"
	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	"github.com/lmittmann/tint"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

type config struct {
	DevMode bool `split_words:"true" default:"true"`

	GrpcAddr    string `split_words:"true" default:"127.0.0.1:8080"`
	HttpAddr    string `split_words:"true" default:"127.0.0.1:8081"`
	DatabaseURL string `split_words:"true" default:"postgres://postgres:postgres@127.0.0.1:5431/postgres?sslmode=disable"`
	StorageURL  string `split_words:"true" default:"filesystem://objstore"`

	Batcher batcher.Config
}

func main() {
	help := flag.Bool("help", false, "Show help")
	version := flag.Bool("version", false, "Show version")
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
	if c.DevMode {
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
	if c.DevMode {
		if err := database.StartPostgresContainer(ctx, c.DatabaseURL); err != nil {
			slog.ErrorContext(ctx, "setting up dev database", "error", err)
			os.Exit(1)
		}
	}
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

	// Server setup
	lis, err := net.Listen("tcp", c.GrpcAddr)
	if err != nil {
		slog.ErrorContext(ctx, "failed to listen for gRPC services", "error", err)
		os.Exit(1)
	}

	// Create shared gRPC server
	srv := grpc.NewServer()

	// Register services
	if c.Batcher.Enabled {
		batcherSrv := batcher.NewHandler(ctx, c.Batcher, database.New(db), store)
		batcherv1.RegisterBatcherServiceServer(srv, batcherSrv)
	}

	// Create errgroup with cancellable context
	g, gCtx := errgroup.WithContext(ctx)

	// Start gRPC server
	g.Go(func() error {
		slog.InfoContext(ctx, "starting gRPC server", "addr", c.GrpcAddr)
		if err := srv.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			return fmt.Errorf("failed to serve gRPC: %w", err)
		}

		return nil
	})

	// Start HTTP metrics server
	g.Go(func() error {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /metrics", promhttp.Handler().ServeHTTP)
		mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(fmt.Sprintf(`{"version": "%s"}`, versioninfo.Short())))
		})
		metricsServer := &http.Server{
			Addr:    c.HttpAddr,
			Handler: mux,
		}

		slog.InfoContext(ctx, "starting metrics server", "addr", c.HttpAddr)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("failed to serve metrics: %w", err)
		}

		return nil
	})

	// Handle shutdown signals
	g.Go(func() error {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		select {
		case sig := <-sigChan:
			slog.InfoContext(ctx, "received shutdown signal", "signal", sig)
			cancel()
		case <-gCtx.Done():
			slog.InfoContext(ctx, "context cancelled, initiating shutdown")
		}

		return nil
	})

	// Start graceful shutdown when any goroutine fails or shutdown is triggered
	go func() {
		<-gCtx.Done()

		// Create separate context for shutdown timeout
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Attempt graceful shutdown of gRPC server
		slog.InfoContext(ctx, "attempting graceful shutdown of gRPC server")
		go srv.GracefulStop()

		// Wait for shutdown or timeout
		select {
		case <-shutdownCtx.Done():
			slog.WarnContext(ctx, "graceful shutdown timed out, forcing stop")
			srv.Stop()
		case <-time.After(100 * time.Millisecond): // Brief pause to allow GracefulStop to complete
			slog.InfoContext(ctx, "gRPC server shutdown gracefully")
		}
	}()

	// Wait for all goroutines to complete
	if err := g.Wait(); err != nil {
		slog.ErrorContext(ctx, "server error", "error", err)
		os.Exit(1)
	}
}
