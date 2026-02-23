// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/events"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage"
)

// PerformancePluginConfig holds S3 configuration for the performance plugin.
// Each telemetry type plugin owns its config independently, allowing different
// buckets, prefixes, or regions per event category.
type PerformancePluginConfig struct {
	Bucket   string // required
	Prefix   string // default "telemetry"
	Region   string // default "us-east-1"
	Endpoint string // optional - for MinIO or custom S3-compatible endpoints
}

// PerformancePlugin stores performance telemetry events as JSON files in Amazon S3.
// It manages its own AWS client and is fully independent of sibling S3 plugins.
type PerformancePlugin struct {
	cfg    PerformancePluginConfig
	client *awss3.Client
	logger *slog.Logger
}

// NewPerformancePlugin creates an S3 plugin for performance events.
func NewPerformancePlugin(cfg PerformancePluginConfig) storage.StoragePlugin[*events.PerformanceEvent] {
	if cfg.Prefix == "" {
		cfg.Prefix = "telemetry"
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	return &PerformancePlugin{cfg: cfg}
}

func (p *PerformancePlugin) Name() string { return "s3:performance" }

func (p *PerformancePlugin) Initialize(ctx context.Context) error {
	p.logger = slog.Default().With("plugin", p.Name())

	if p.cfg.Bucket == "" {
		return fmt.Errorf("s3 bucket is required")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(p.cfg.Region))
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Configure S3 client with custom endpoint for MinIO or S3-compatible services
	if p.cfg.Endpoint != "" {
		p.client = awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
			o.BaseEndpoint = aws.String(p.cfg.Endpoint)
			o.UsePathStyle = true // MinIO requires path-style addressing
		})
		p.logger.Info("s3 plugin initialized with custom endpoint",
			"bucket", p.cfg.Bucket, "prefix", p.cfg.Prefix, "region", p.cfg.Region, "endpoint", p.cfg.Endpoint)
	} else {
		p.client = awss3.NewFromConfig(awsCfg)
		p.logger.Info("s3 plugin initialized",
			"bucket", p.cfg.Bucket, "prefix", p.cfg.Prefix, "region", p.cfg.Region)
	}
	return nil
}

// Filter determines if an event should be processed by this plugin.
// ------------------------------------------------------------------------------
// DEVELOPER NOTE:
// Implement custom filtering logic here. Return false to skip an event.
// For example, filter out events from certain namespaces or users.
// ------------------------------------------------------------------------------
func (p *PerformancePlugin) Filter(_ *events.PerformanceEvent) bool { return true }

// WriteBatch serializes the batch to JSON and uploads it to S3.
// ------------------------------------------------------------------------------
// DEVELOPER NOTE:
// Customize transform logic here to reshape or enrich events before upload.
// The S3 key format is: {prefix}/performance/{namespace}/year=YYYY/month=MM/day=DD/{ts}.json
// ------------------------------------------------------------------------------
func (p *PerformancePlugin) WriteBatch(ctx context.Context, evts []*events.PerformanceEvent) (int, error) {
	if len(evts) == 0 {
		return 0, nil
	}

	documents := make([]interface{}, 0, len(evts))
	for _, e := range evts {
		documents = append(documents, e.ToDocument())
	}

	data, err := json.MarshalIndent(documents, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("failed to marshal events: %w", err)
	}

	now := time.Now().UTC()
	key := fmt.Sprintf("%s/performance/%s/year=%d/month=%02d/day=%02d/%d.json",
		p.cfg.Prefix, evts[0].Namespace,
		now.Year(), now.Month(), now.Day(), now.UnixMilli())

	_, err = p.client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:      aws.String(p.cfg.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
		Metadata: map[string]string{
			"namespace":   evts[0].Namespace,
			"event_count": fmt.Sprintf("%d", len(documents)),
			"created_at":  now.Format(time.RFC3339),
		},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to upload to S3: %w", err)
	}

	p.logger.Info("batch written to S3", "key", key, "count", len(documents), "size_bytes", len(data))
	return len(documents), nil
}

func (p *PerformancePlugin) Close() error {
	// The AWS S3 client does not hold persistent connections and requires no cleanup.
	p.logger.Info("s3 plugin closed")
	return nil
}

func (p *PerformancePlugin) HealthCheck(ctx context.Context) error {
	_, err := p.client.HeadBucket(ctx, &awss3.HeadBucketInput{
		Bucket: aws.String(p.cfg.Bucket),
	})
	if err != nil {
		return fmt.Errorf("S3 health check failed: %w", err)
	}
	return nil
}
