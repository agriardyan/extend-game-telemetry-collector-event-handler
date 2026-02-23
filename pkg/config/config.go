// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v10"
)

// Config is the root configuration structure
type Config struct {
	ABExtend      ABExtendConfig      `envPrefix:"AB_"`
	Server        ServerConfig        `envPrefix:"SERVER_"`
	Processor     ProcessorConfig     `envPrefix:"PROCESSOR_"`
	Deduplication DeduplicationConfig `envPrefix:"DEDUP_"`
	Storage       StorageConfig       `envPrefix:"STORAGE_"`
}

type ABExtendConfig struct {
	BaseURL      string `env:"BASE_URL,required" envDefault:"https://api.accelbyte.io"`
	ClientID     string `env:"CLIENT_ID,required"`
	ClientSecret string `env:"CLIENT_SECRET,required"`
	Namespace    string `env:"NAMESPACE,required"`
}

// ServerConfig holds server-related settings
type ServerConfig struct {
	GRPCPort int    `env:"GRPC_PORT" envDefault:"6565"`
	HTTPPort int    `env:"HTTP_PORT" envDefault:"8000"`
	BasePath string `env:"BASE_PATH" envDefault:"/telemetry"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
}

// ProcessorConfig holds async processing settings
type ProcessorConfig struct {
	Workers              int           `env:"WORKERS" envDefault:"10"`
	ChannelBuffer        int           `env:"CHANNEL_BUFFER" envDefault:"10000"`
	DefaultBatchSize     int           `env:"DEFAULT_BATCH_SIZE" envDefault:"100"`
	DefaultFlushInterval time.Duration `env:"DEFAULT_FLUSH_INTERVAL" envDefault:"5s"`
}

// DeduplicationConfig holds deduplication settings
type DeduplicationConfig struct {
	Enabled bool          `env:"ENABLED" envDefault:"false"`
	Type    string        `env:"TYPE" envDefault:"noop"` // "memory", "redis", or "noop"
	TTL     time.Duration `env:"TTL" envDefault:"1h"`
	Redis   RedisConfig   `envPrefix:"REDIS_"`
}

// RedisConfig holds Redis connection settings
type RedisConfig struct {
	Addr     string `env:"ADDR" envDefault:"localhost:6379"`
	Password string `env:"PASSWORD" envDefault:""`
	DB       int    `env:"DB" envDefault:"0"`
}

// StorageConfig holds storage plugin configurations
type StorageConfig struct {
	// Comma-separated list of enabled plugins
	EnabledPlugins string `env:"ENABLED_PLUGINS" envDefault:"noop"`

	// Plugin-specific configurations
	Noop     PluginConfig `envPrefix:"NOOP_"`
	S3       PluginConfig `envPrefix:"S3_"`
	Postgres PluginConfig `envPrefix:"POSTGRES_"`
	Kafka    PluginConfig `envPrefix:"KAFKA_"`
	MongoDB  PluginConfig `envPrefix:"MONGODB_"`
}

// PluginConfig holds configuration for a single storage plugin
type PluginConfig struct {
	BatchSize     int           `env:"BATCH_SIZE" envDefault:"100"`
	FlushInterval time.Duration `env:"FLUSH_INTERVAL" envDefault:"5s"`
	Workers       int           `env:"WORKERS" envDefault:"2"`
	Retry         RetryConfig   `envPrefix:"RETRY_"`

	// Plugin-specific settings
	// S3 settings
	S3Bucket   string `env:"BUCKET" envDefault:""`
	S3Prefix   string `env:"PREFIX" envDefault:"telemetry"`
	S3Region   string `env:"REGION" envDefault:"us-east-1"`
	S3Endpoint string `env:"ENDPOINT" envDefault:""` // for MinIO or custom S3-compatible endpoints

	// Postgres settings
	PostgresDSN   string `env:"DSN" envDefault:""`
	PostgresTable string `env:"TABLE" envDefault:"telemetry_events"`

	// Kafka settings
	KafkaBrokers     string `env:"BROKERS" envDefault:"localhost:9092"`
	KafkaTopic       string `env:"TOPIC" envDefault:"game-telemetry"`
	KafkaCompression string `env:"COMPRESSION" envDefault:"snappy"`

	// MongoDB settings
	MongoURI        string `env:"URI" envDefault:"mongodb://localhost:27017"`
	MongoDatabase   string `env:"DATABASE" envDefault:"telemetry"`
	MongoCollection string `env:"COLLECTION" envDefault:"events"`
}

// RetryConfig holds retry policy settings
type RetryConfig struct {
	MaxRetries     int           `env:"MAX_RETRIES" envDefault:"3"`
	InitialBackoff time.Duration `env:"INITIAL_BACKOFF" envDefault:"100ms"`
	MaxBackoff     time.Duration `env:"MAX_BACKOFF" envDefault:"10s"`
	Multiplier     float64       `env:"MULTIPLIER" envDefault:"2.0"`
}

// LoadConfig loads configuration from environment variables
func LoadConfig() (*Config, error) {
	cfg := &Config{}

	// Parse environment variables
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config from environment: %w", err)
	}

	// Validate configuration
	if err := ValidateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// GetEnabledPlugins returns the names of enabled storage plugins
// in the order they appear in STORAGE_ENABLED_PLUGINS.
func (c *Config) GetEnabledPlugins() []string {
	var names []string
	for _, name := range strings.Split(c.Storage.EnabledPlugins, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// ValidateConfig validates the configuration for common errors
func ValidateConfig(config *Config) error {
	// Validate server config
	if config.Server.GRPCPort <= 0 || config.Server.GRPCPort > 65535 {
		return fmt.Errorf("invalid GRPC port: %d", config.Server.GRPCPort)
	}
	if config.Server.HTTPPort <= 0 || config.Server.HTTPPort > 65535 {
		return fmt.Errorf("invalid HTTP port: %d", config.Server.HTTPPort)
	}
	if config.Server.BasePath == "" {
		return fmt.Errorf("base_path cannot be empty")
	}

	// Validate processor config
	if config.Processor.Workers <= 0 {
		return fmt.Errorf("processor workers must be > 0")
	}
	if config.Processor.ChannelBuffer <= 0 {
		return fmt.Errorf("processor channel buffer must be > 0")
	}

	// Validate deduplication config
	if config.Deduplication.Enabled {
		validTypes := map[string]bool{"memory": true, "redis": true, "noop": true}
		if !validTypes[config.Deduplication.Type] {
			return fmt.Errorf("invalid deduplication type: %s (must be memory, redis, or noop)", config.Deduplication.Type)
		}

		if config.Deduplication.Type == "redis" && config.Deduplication.Redis.Addr == "" {
			return fmt.Errorf("redis addr is required when deduplication type is redis")
		}
	}

	// Validate storage plugins
	if strings.TrimSpace(config.Storage.EnabledPlugins) == "" {
		return fmt.Errorf("at least one storage plugin must be enabled (STORAGE_ENABLED_PLUGINS)")
	}

	return nil
}
