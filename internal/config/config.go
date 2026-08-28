package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	TailscaleAuthKey      string
	OracleVMTailscaleHost string
	SchedulerGRPCPort     string
	B2AppKeyID            string
	B2AppKey              string
	B2BucketName          string
	MinioEndpoint         string
	MinioAccessKey        string
	MinioSecretKey        string
	NATSUrl               string
	SQLiteDBPath          string
	GitHubActionsToken    string
	GitHubActionsRepo     string
	CircleCIToken         string
	CircleCIProjectSlug   string
	LogLevel              string
}

func LoadConfig(ctx context.Context) (*Config, error) {
	home, _ := os.UserHomeDir()
	globalConfig := ""
	if home != "" {
		globalConfig = home + "/.packets/config.env"
	}
	_ = godotenv.Load(".env.local", ".env", globalConfig, "/etc/packets/env")

	cfg := &Config{
		TailscaleAuthKey:      os.Getenv("TAILSCALE_AUTH_KEY"),
		OracleVMTailscaleHost: os.Getenv("ORACLE_VM_TAILSCALE_HOSTNAME"),
		SchedulerGRPCPort:     os.Getenv("SCHEDULER_GRPC_PORT"),
		B2AppKeyID:            os.Getenv("B2_APPLICATION_KEY_ID"),
		B2AppKey:              os.Getenv("B2_APPLICATION_KEY"),
		B2BucketName:          os.Getenv("B2_BUCKET_NAME"),
		MinioEndpoint:         os.Getenv("MINIO_ENDPOINT"),
		MinioAccessKey:        os.Getenv("MINIO_ACCESS_KEY"),
		MinioSecretKey:        os.Getenv("MINIO_SECRET_KEY"),
		NATSUrl:               os.Getenv("NATS_URL"),
		SQLiteDBPath:          os.Getenv("SQLITE_DB_PATH"),
		GitHubActionsToken:    os.Getenv("GITHUB_ACTIONS_TOKEN"),
		GitHubActionsRepo:     os.Getenv("GITHUB_ACTIONS_REPO"),
		CircleCIToken:         os.Getenv("CIRCLECI_TOKEN"),
		CircleCIProjectSlug:   os.Getenv("CIRCLECI_PROJECT_SLUG"),
		LogLevel:              os.Getenv("LOG_LEVEL"),
	}

	if cfg.SchedulerGRPCPort == "" {
		cfg.SchedulerGRPCPort = "50051"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}

	return cfg, nil
}

func (c *Config) ValidateForDaemon() error {
	if c.SQLiteDBPath == "" {
		return fmt.Errorf("SQLITE_DB_PATH is required")
	}
	if c.NATSUrl == "" {
		return fmt.Errorf("NATS_URL is required")
	}
	return nil
}

func (c *Config) SchedulerAddr() string {
	return ":" + c.SchedulerGRPCPort
}

func (c *Config) ParseLogLevel() slog.Level {
	switch c.LogLevel {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
