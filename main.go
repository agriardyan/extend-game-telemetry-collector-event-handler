// Copyright (c) 2023-2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/internal/app"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create and initialize application
	application, err := app.New(ctx)
	if err != nil {
		slog.Error("failed to initialize application", "error", err)
		os.Exit(1)
	}

	// Run application
	if err := application.Run(ctx); err != nil {
		slog.Error("failed to start application", "error", err)
		os.Exit(1)
	}

	// Wait for shutdown signal
	shutdownCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-shutdownCtx.Done()
	slog.Info("shutdown signal received")

	// Graceful shutdown
	application.Shutdown(ctx)
}
