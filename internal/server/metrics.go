// Copyright (c) 2023-2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	prometheusGrpc "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus"
	prometheusCollectors "github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	MetricsEndpoint = "/metrics"
	MetricsPort     = 8080
)

// StartMetricsServer starts the Prometheus metrics HTTP server
func StartMetricsServer(port int, logger *slog.Logger) error {
	// Register Prometheus Metrics
	prometheusRegistry := prometheus.NewRegistry()
	prometheusRegistry.MustRegister(
		prometheusCollectors.NewGoCollector(),
		prometheusCollectors.NewProcessCollector(prometheusCollectors.ProcessCollectorOpts{}),
		prometheusGrpc.DefaultServerMetrics,
	)

	go func() {
		http.Handle(MetricsEndpoint, promhttp.HandlerFor(prometheusRegistry, promhttp.HandlerOpts{}))
		if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
			logger.Error("failed to start metrics server", "error", err)
			os.Exit(1)
		}
	}()

	logger.Info("serving prometheus metrics", "port", port, "endpoint", MetricsEndpoint)
	return nil
}
