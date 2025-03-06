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
	"connectrpc.com/grpchealth"
	"connectrpc.com/grpcreflect"
	"connectrpc.com/otelconnect"
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

	batcherv1connect "github.com/dynoinc/skyvault/gen/proto/batcher/v1/v1connect"
	cachev1connect "github.com/dynoinc/skyvault/gen/proto/cache/v1/v1connect"
	indexv1connect "github.com/dynoinc/skyvault/gen/proto/index/v1/v1connect"
	orchestratorv1connect "github.com/dynoinc/skyvault/gen/proto/orchestrator/v1/v1connect"
	workerv1connect "github.com/dynoinc/skyvault/gen/proto/worker/v1/v1connect"
	"github.com/dynoinc/skyvault/internal/batcher"
	"github.com/dynoinc/skyvault/internal/cache"
	"github.com/dynoinc/skyvault/internal/database"
	"github.com/dynoinc/skyvault/internal/index"
	"github.com/dynoinc/skyvault/internal/middleware"
	"github.com/dynoinc/skyvault/internal/orchestrator"
	"github.com/dynoinc/skyvault/internal/storage"
	"github.com/dynoinc/skyvault/internal/worker"
)

type config struct {
	Addr        string `split_words:"true" default:"127.0.0.1:5001"`
	DatabaseURL string `split_words:"true" default:"postgres://postgres:postgres@127.0.0.1:5431/postgres?sslmode=disable"`
	StorageURL  string `split_words:"true" default:"filesystem://objstore"`

	Batcher      batcher.Config
	Cache        cache.Config
	Index        index.Config
	Orchestrator orchestrator.Config
	Worker       worker.Config
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

	otelInterceptor, err := otelconnect.NewInterceptor(otelconnect.WithTrustRemote())
	if err != nil {
		slog.ErrorContext(ctx, "setting up OpenTelemetry interceptor", "error", err)
		os.Exit(1)
	}

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
		otelInterceptor,
	)

	// Create the gRPC health checker
	healthSvc := grpchealth.NewStaticChecker()

	// Collect enabled service names for reflection
	var enabledServices []string

	// Register Connect service handlers
	if c.Batcher.Enabled {
		batcherHandler := batcher.NewHandler(ctx, c.Batcher, database.New(db), store)
		path, handler := batcherv1connect.NewBatcherServiceHandler(
			batcherHandler,
			interceptors,
		)
		mux.Handle(path, handler)

		// Set health status for BatcherService
		healthSvc.SetStatus(batcherv1connect.BatcherServiceName, grpchealth.StatusServing)
		slog.InfoContext(ctx, "BatcherService enabled and healthy", "service", batcherv1connect.BatcherServiceName)
		enabledServices = append(enabledServices, batcherv1connect.BatcherServiceName)
	}

	if c.Cache.Enabled {
		cacheHandler := cache.NewHandler(ctx, c.Cache, store)
		path, handler := cachev1connect.NewCacheServiceHandler(
			cacheHandler,
			interceptors,
		)
		mux.Handle(path, handler)

		// Set health status for CacheService
		healthSvc.SetStatus(cachev1connect.CacheServiceName, grpchealth.StatusServing)
		slog.InfoContext(ctx, "CacheService enabled and healthy", "service", cachev1connect.CacheServiceName)
		enabledServices = append(enabledServices, cachev1connect.CacheServiceName)
	}

	if c.Index.Enabled {
		indexHandler, err := index.NewHandler(ctx, c.Index, db)
		if err != nil {
			slog.ErrorContext(ctx, "setting up index", "error", err)
			os.Exit(1)
		}

		path, handler := indexv1connect.NewIndexServiceHandler(
			indexHandler,
			interceptors,
		)
		mux.Handle(path, handler)

		// Set health status for IndexService
		healthSvc.SetStatus(indexv1connect.IndexServiceName, grpchealth.StatusServing)
		slog.InfoContext(ctx, "IndexService enabled and healthy", "service", indexv1connect.IndexServiceName)
		enabledServices = append(enabledServices, indexv1connect.IndexServiceName)
	}

	if c.Orchestrator.Enabled {
		orchestratorHandler, err := orchestrator.NewHandler(ctx, c.Orchestrator, db)
		if err != nil {
			slog.ErrorContext(ctx, "setting up orchestrator", "error", err)
			os.Exit(1)
		}

		path, handler := orchestratorv1connect.NewOrchestratorServiceHandler(
			orchestratorHandler,
			interceptors,
		)
		mux.Handle(path, handler)

		// Set health status for OrchestratorService
		healthSvc.SetStatus(orchestratorv1connect.OrchestratorServiceName, grpchealth.StatusServing)
		slog.InfoContext(ctx, "OrchestratorService enabled and healthy", "service", orchestratorv1connect.OrchestratorServiceName)
		enabledServices = append(enabledServices, orchestratorv1connect.OrchestratorServiceName)
	}

	if c.Worker.Enabled {
		workerHandler, err := worker.NewHandler(ctx, c.Worker, db, store)
		if err != nil {
			slog.ErrorContext(ctx, "setting up worker", "error", err)
			os.Exit(1)
		}

		path, handler := workerv1connect.NewWorkerServiceHandler(
			workerHandler,
			interceptors,
		)
		mux.Handle(path, handler)

		// Set health status for WorkerService
		healthSvc.SetStatus(workerv1connect.WorkerServiceName, grpchealth.StatusServing)
		slog.InfoContext(ctx, "WorkerService enabled and healthy", "service", workerv1connect.WorkerServiceName)
		enabledServices = append(enabledServices, workerv1connect.WorkerServiceName)
	}

	// Set the overall service status
	healthSvc.SetStatus("", grpchealth.StatusServing)

	// Register the health check service
	healthPath, healthHandler := grpchealth.NewHandler(healthSvc)
	mux.Handle(healthPath, healthHandler)
	slog.InfoContext(ctx, "gRPC health check service initialized", "path", healthPath)

	// Register gRPC reflection service with only enabled services
	reflector := grpcreflect.NewStaticReflector(
		append(enabledServices, grpchealth.HealthV1ServiceName)...,
	)
	reflectPath, reflectHandler := grpcreflect.NewHandlerV1(reflector)
	mux.Handle(reflectPath, reflectHandler)
	slog.InfoContext(ctx, "gRPC reflection service initialized",
		"path_v1", reflectPath,
		"enabled_services", enabledServices,
	)

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
