// Copyright (c) 2023-2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package common

import (
	"os"
	"time"

	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/sdk/resource"

	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
	semanticConventions "go.opentelemetry.io/otel/semconv/v1.12.0"
)

func NewTracerProvider(serviceName string) (*sdkTrace.TracerProvider, error) {
	zipkinEndpoint := "http://localhost:9411/api/v2/spans"
	if val := os.Getenv("OTEL_EXPORTER_ZIPKIN_ENDPOINT"); val != "" {
		zipkinEndpoint = val
	}
	exporter, err := zipkin.New(zipkinEndpoint)
	if err != nil {
		return nil, err
	}

	res := resource.NewWithAttributes(
		semanticConventions.SchemaURL,
		semanticConventions.ServiceNameKey.String(serviceName),
	)

	return sdkTrace.NewTracerProvider(
		sdkTrace.WithBatcher(exporter, sdkTrace.WithBatchTimeout(time.Second*1)),
		sdkTrace.WithResource(res),
		sdkTrace.WithSampler(sdkTrace.AlwaysSample()),
	), nil
}
