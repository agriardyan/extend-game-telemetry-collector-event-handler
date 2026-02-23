// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/events"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/storage"
)

// OauthTokenGeneratedPluginConfig holds Kafka configuration for the oauth_token_generated plugin.
type OauthTokenGeneratedPluginConfig struct {
	Brokers       []string
	Topic         string
	Compression   string // "snappy" (default), "gzip", "lz4", "zstd"
	BatchSize     int
	FlushInterval time.Duration
}

// OauthTokenGeneratedPlugin streams oauth token generated events to a Kafka topic.
type OauthTokenGeneratedPlugin struct {
	cfg    OauthTokenGeneratedPluginConfig
	writer *kafkago.Writer
	logger *slog.Logger
}

// NewOauthTokenGeneratedPlugin creates a Kafka plugin for oauth_token_generated events.
func NewOauthTokenGeneratedPlugin(cfg OauthTokenGeneratedPluginConfig) storage.StoragePlugin[*events.OauthTokenGeneratedEvent] {
	return &OauthTokenGeneratedPlugin{cfg: cfg}
}

func (p *OauthTokenGeneratedPlugin) Name() string { return "kafka:oauth_token_generated" }

func (p *OauthTokenGeneratedPlugin) Initialize(_ context.Context) error {
	p.logger = slog.Default().With("plugin", p.Name())
	if p.cfg.Compression == "" {
		p.cfg.Compression = "snappy"
	}
	p.writer = &kafkago.Writer{
		Addr:         kafkago.TCP(p.cfg.Brokers...),
		Topic:        p.cfg.Topic,
		Balancer:     &kafkago.Hash{},
		BatchSize:    p.cfg.BatchSize,
		BatchTimeout: p.cfg.FlushInterval,
		Compression:  compressionCodec(p.cfg.Compression),
		Async:        false,
		RequiredAcks: kafkago.RequireAll,
	}
	p.logger.Info("kafka plugin initialized", "topic", p.cfg.Topic, "brokers", p.cfg.Brokers)
	return nil
}

// Filter determines if an event should be processed by this plugin.
// ------------------------------------------------------------------------------
// DEVELOPER NOTE:
// Implement custom filtering logic here. Return false to skip an event.
// For example, filter out events from certain namespaces or users.
// ------------------------------------------------------------------------------
func (p *OauthTokenGeneratedPlugin) Filter(_ *events.OauthTokenGeneratedEvent) bool { return true }

func (p *OauthTokenGeneratedPlugin) transform(e *events.OauthTokenGeneratedEvent) ([]byte, error) {
	data, err := json.Marshal(e.ToDocument())
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event: %w", err)
	}
	return data, nil
}

func (p *OauthTokenGeneratedPlugin) WriteBatch(ctx context.Context, evts []*events.OauthTokenGeneratedEvent) (int, error) {
	if len(evts) == 0 {
		return 0, nil
	}

	messages := make([]kafkago.Message, 0, len(evts))
	for _, e := range evts {
		data, err := p.transform(e)
		if err != nil {
			p.logger.Warn("failed to transform event, skipping", "error", err, "user_id", e.UserID)
			continue
		}
		messages = append(messages, kafkago.Message{
			Key:   []byte(e.UserID),
			Value: data,
			Time:  time.UnixMilli(e.ServerTimestamp),
			Headers: []kafkago.Header{
				{Key: "namespace", Value: []byte(e.Namespace)},
				{Key: "kind", Value: []byte("oauth_token_generated")},
			},
		})
	}

	if len(messages) == 0 {
		return 0, fmt.Errorf("all events failed transformation")
	}

	if err := p.writer.WriteMessages(ctx, messages...); err != nil {
		return 0, fmt.Errorf("failed to write messages to Kafka: %w", err)
	}

	p.logger.Info("batch written to kafka", "topic", p.cfg.Topic, "count", len(messages))
	return len(messages), nil
}

func (p *OauthTokenGeneratedPlugin) Close() error {
	p.logger.Info("kafka plugin closing", "topic", p.cfg.Topic)
	if p.writer != nil {
		return p.writer.Close()
	}
	return nil
}

func (p *OauthTokenGeneratedPlugin) HealthCheck(ctx context.Context) error {
	if len(p.cfg.Brokers) == 0 {
		return fmt.Errorf("no kafka brokers configured")
	}
	conn, err := kafkago.DialContext(ctx, "tcp", p.cfg.Brokers[0])
	if err != nil {
		return fmt.Errorf("kafka health check failed: %w", err)
	}
	defer conn.Close()
	partitions, err := conn.ReadPartitions(p.cfg.Topic)
	if err != nil {
		return fmt.Errorf("failed to read topic partitions: %w", err)
	}
	if len(partitions) == 0 {
		return fmt.Errorf("topic %s has no partitions", p.cfg.Topic)
	}
	return nil
}
