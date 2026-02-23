// Copyright (c) 2023-2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/common"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// SetupTelemetry initializes OpenTelemetry tracer and propagators
func SetupTelemetry(ctx context.Context, serviceName string, logger *slog.Logger) (func(context.Context) error, error) {
	// Set service name from environment if available
	if val := os.Getenv("OTEL_SERVICE_NAME"); val != "" {
		serviceName = "extend-app-se-" + strings.ToLower(val)
	}

	tracerProvider, err := common.NewTracerProvider(serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to create tracer provider: %w", err)
	}
	otel.SetTracerProvider(tracerProvider)

	// Set Text Map Propagator
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			b3.New(),
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	logger.Info("telemetry initialized", "service", serviceName)

	// Return shutdown function
	shutdownFunc := func(ctx context.Context) error {
		return tracerProvider.Shutdown(ctx)
	}

	return shutdownFunc, nil
}
