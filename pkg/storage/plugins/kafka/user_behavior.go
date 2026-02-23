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

// UserBehaviorPluginConfig holds Kafka configuration for the user behavior plugin.
// Each telemetry type plugin owns its config independently, allowing different
// broker clusters, topics, or compression settings per event category.
type UserBehaviorPluginConfig struct {
	Brokers       []string
	Topic         string
	Compression   string // "snappy" (default), "gzip", "lz4", "zstd"
	BatchSize     int
	FlushInterval time.Duration
}

// UserBehaviorPlugin streams user behavior telemetry events to a Kafka topic.
// It manages its own kafka.Writer and is fully independent of sibling Kafka plugins.
type UserBehaviorPlugin struct {
	cfg    UserBehaviorPluginConfig
	writer *kafkago.Writer
	logger *slog.Logger
}

// NewUserBehaviorPlugin creates a Kafka plugin for user behavior events.
func NewUserBehaviorPlugin(cfg UserBehaviorPluginConfig) storage.StoragePlugin[*events.UserBehaviorEvent] {
	return &UserBehaviorPlugin{cfg: cfg}
}

func (p *UserBehaviorPlugin) Name() string { return "kafka:user_behavior" }

func (p *UserBehaviorPlugin) Initialize(_ context.Context) error {
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
func (p *UserBehaviorPlugin) Filter(_ *events.UserBehaviorEvent) bool { return true }

// transform serializes a UserBehaviorEvent to JSON for the Kafka message value.
// ------------------------------------------------------------------------------
// DEVELOPER NOTE:
// Customize this method to reshape or enrich events before publishing.
// You can also change the message key strategy (currently partitioned by user_id).
// ------------------------------------------------------------------------------
func (p *UserBehaviorPlugin) transform(e *events.UserBehaviorEvent) ([]byte, error) {
	data, err := json.Marshal(e.ToDocument())
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event: %w", err)
	}
	return data, nil
}

func (p *UserBehaviorPlugin) WriteBatch(ctx context.Context, evts []*events.UserBehaviorEvent) (int, error) {
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
				{Key: "kind", Value: []byte("user_behavior")},
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

func (p *UserBehaviorPlugin) Close() error {
	p.logger.Info("kafka plugin closing", "topic", p.cfg.Topic)
	if p.writer != nil {
		return p.writer.Close()
	}
	return nil
}

func (p *UserBehaviorPlugin) HealthCheck(ctx context.Context) error {
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

// compressionCodec maps a codec name string to kafka.Compression.
//
// NOTE: This function is specific to the Kafka transport and is intentionally
// kept within this package rather than in a shared file. Go's package system
// scopes functions to the package (not the file), so this single definition
// is available to all telemetry-type plugins in this package. If you add a
// new telemetry type Kafka plugin, it can use this function directly — no
// import or copy needed.
func compressionCodec(name string) kafkago.Compression {
	switch name {
	case "gzip":
		return kafkago.Gzip
	case "snappy":
		return kafkago.Snappy
	case "lz4":
		return kafkago.Lz4
	case "zstd":
		return kafkago.Zstd
	default:
		return kafkago.Snappy
	}
}
