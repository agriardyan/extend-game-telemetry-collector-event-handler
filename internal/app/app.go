// Copyright (c) 2023-2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/internal/bootstrap"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/internal/server"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/config"
	oauthpb "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/pb/accelbyte-asyncapi/iam/oauth/v1"
	statistic "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/pb/accelbyte-asyncapi/social/statistic/v1"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/service"
	"google.golang.org/grpc"
)

// Application represents the main application with all its components
type Application struct {
	config         *config.Config
	logger         *slog.Logger
	iamServices    *bootstrap.IAMServices
	plugins        *bootstrap.StoragePlugins
	deduplicators  *bootstrap.Deduplicators
	processors     *bootstrap.Processors
	grpcServer     *grpc.Server
	shutdownTracer func(context.Context) error
}

// New creates and initializes a new Application
func New(ctx context.Context) (*Application, error) {
	app := &Application{}

	// Load configuration
	appCfg, err := config.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}
	app.config = appCfg

	// Setup logger
	app.logger = setupLogger(appCfg.Server.LogLevel)
	app.logger.Info("configuration loaded from environment variables")

	// Initialize IAM
	iamServices, err := bootstrap.InitializeIAM(ctx, app.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize IAM: %w", err)
	}
	app.iamServices = iamServices

	// Setup telemetry (tracing)
	shutdownTracer, err := server.SetupTelemetry(ctx, "extend-app-service-extension", app.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to setup telemetry: %w", err)
	}
	app.shutdownTracer = shutdownTracer

	// Initialize storage plugins
	plugins, err := bootstrap.InitializeStoragePlugins(ctx, appCfg, app.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage plugins: %w", err)
	}
	app.plugins = plugins

	// Initialize deduplicators
	app.deduplicators = bootstrap.InitializeDeduplicators(appCfg, app.logger)

	// Initialize and start processors
	app.processors = bootstrap.InitializeProcessors(appCfg, plugins, app.deduplicators, app.logger)

	// Create gRPC server
	grpcServer, err := server.NewGRPCServer(ctx, server.GRPCServerConfig{
		Port:         server.GRPCServerPort,
		Logger:       app.logger,
		OAuthService: iamServices.OAuthService,
		TokenRepo:    iamServices.TokenRepo,
		ConfigRepo:   iamServices.ConfigRepo,
		RefreshRepo:  iamServices.RefreshRepo,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC server: %w", err)
	}
	app.grpcServer = grpcServer

	// Register StatItemUpdated event handler
	statItemUpdatedSvc := service.NewStatItemUpdatedService(
		bootstrap.GetNamespace(),
		app.processors.StatItemUpdated,
		app.logger,
	)
	statistic.RegisterStatisticStatItemUpdatedServiceServer(grpcServer, statItemUpdatedSvc)

	// Register OauthTokenGenerated event handler
	oauthTokenGeneratedSvc := service.NewOauthTokenGeneratedService(
		bootstrap.GetNamespace(),
		app.processors.OauthTokenGenerated,
		app.logger,
	)
	oauthpb.RegisterOauthTokenOauthTokenGeneratedServiceServer(grpcServer, oauthTokenGeneratedSvc)

	return app, nil
}

// Run starts all application services
func (app *Application) Run(ctx context.Context) error {
	// Start metrics server
	if err := server.StartMetricsServer(server.MetricsPort, app.logger); err != nil {
		return fmt.Errorf("failed to start metrics server: %w", err)
	}

	// Start gRPC server
	if err := server.StartGRPCServer(app.grpcServer, server.GRPCServerPort, app.logger); err != nil {
		return fmt.Errorf("failed to start gRPC server: %w", err)
	}

	// Determine base path
	basePath := os.Getenv("SERVER_BASE_PATH")
	if basePath == "" {
		basePath = os.Getenv("BASE_PATH")
	}
	if basePath == "" {
		basePath = "/telemetry"
	}

	app.logger.Info("application started successfully")
	return nil
}

// Shutdown performs graceful shutdown of all application components
func (app *Application) Shutdown(ctx context.Context) {
	app.logger.Info("initiating graceful shutdown")

	shutdownTimeout := 30 * time.Second

	// Shutdown processors
	bootstrap.ShutdownProcessors(app.processors, shutdownTimeout, app.logger)

	// Close deduplicators
	bootstrap.CloseDeduplicators(app.deduplicators, app.logger)

	// Close storage plugins
	bootstrap.CloseStoragePlugins(app.plugins, app.logger)

	// Shutdown tracer
	if app.shutdownTracer != nil {
		if err := app.shutdownTracer(ctx); err != nil {
			app.logger.Error("failed to shutdown tracer provider", "error", err)
		}
	}

	app.logger.Info("graceful shutdown complete")
}

func setupLogger(logLevel string) *slog.Logger {
	slogLevel := parseSlogLevel(logLevel)
	opts := &slog.HandlerOptions{Level: slogLevel}
	handler := slog.NewJSONHandler(os.Stdout, opts)
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func parseSlogLevel(levelStr string) slog.Level {
	switch strings.ToLower(levelStr) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "fatal", "panic":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
