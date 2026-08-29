package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	TailscaleAuthKey      string
	OracleVMTailscaleHost string
	SchedulerGRPCPort     string
	NATSUrl               string
	SQLiteDBPath          string
	GitHubActionsToken    string
	GitHubActionsRepo     string
	CircleCIToken         string
	CircleCIProjectSlug   string
	LogLevel              string
	DirectCIMode          bool

	ObjectStoreType           string
	ObjectStoreBucket         string
	ObjectStoreRegion         string
	ObjectStoreEndpoint       string
	ObjectStoreAccessKey      string
	ObjectStoreSecretKey      string
	ObjectStoreForcePathStyle bool

	MaxWorkspaceSizeMB int64
	MaxArtifactSizeMB  int64
	WorkspaceTempDir   string

	DefaultRunner string

	GitHubToken        string
	GitHubWorkflowRepo string

	TLSEnabled               bool
	TLSCertFile              string
	TLSKeyFile               string
	TLSCAFile                string
	TLSInsecureSkipVerify    bool
	MaxConcurrentJobsPerUser int
	MaxSubmissionsPerMinute  int
}

func LoadConfig(ctx context.Context) (*Config, error) {
	home, _ := os.UserHomeDir()
	globalConfig := ""
	if home != "" {
		globalConfig = home + "/.packets/config.env"
	}
	_ = godotenv.Load(".env.local", ".env", globalConfig, "/etc/packets/env")

	maxWS, _ := strconv.ParseInt(os.Getenv("PACKETS_MAX_WORKSPACE_MB"), 10, 64)
	if maxWS == 0 {
		maxWS = 500
	}
	maxArt, _ := strconv.ParseInt(os.Getenv("PACKETS_MAX_ARTIFACT_MB"), 10, 64)
	if maxArt == 0 {
		maxArt = 1000
	}

	forcePathStyle := os.Getenv("PACKETS_S3_FORCE_PATH_STYLE") == "true" || os.Getenv("PACKETS_S3_FORCE_PATH_STYLE") == "1"
	tlsEnabled := os.Getenv("PACKETS_TLS_ENABLED") == "true" || os.Getenv("PACKETS_TLS_ENABLED") == "1"
	tlsInsecure := os.Getenv("PACKETS_TLS_INSECURE_SKIP_VERIFY") == "true" || os.Getenv("PACKETS_TLS_INSECURE_SKIP_VERIFY") == "1"

	maxConc, _ := strconv.Atoi(os.Getenv("PACKETS_MAX_CONCURRENT_JOBS"))
	if maxConc <= 0 {
		maxConc = 5
	}
	maxRate, _ := strconv.Atoi(os.Getenv("PACKETS_MAX_RATE_PER_MINUTE"))
	if maxRate <= 0 {
		maxRate = 60
	}

	cfg := &Config{
		TailscaleAuthKey:      os.Getenv("TAILSCALE_AUTH_KEY"),
		OracleVMTailscaleHost: os.Getenv("ORACLE_VM_TAILSCALE_HOSTNAME"),
		SchedulerGRPCPort:     os.Getenv("SCHEDULER_GRPC_PORT"),
		NATSUrl:               os.Getenv("NATS_URL"),
		SQLiteDBPath:          os.Getenv("SQLITE_DB_PATH"),
		GitHubActionsToken:    os.Getenv("GITHUB_ACTIONS_TOKEN"),
		GitHubActionsRepo:     os.Getenv("GITHUB_ACTIONS_REPO"),
		CircleCIToken:         os.Getenv("CIRCLECI_TOKEN"),
		CircleCIProjectSlug:   os.Getenv("CIRCLECI_PROJECT_SLUG"),
		LogLevel:              os.Getenv("LOG_LEVEL"),
		DirectCIMode:          os.Getenv("DIRECT_CI_MODE") == "true" || os.Getenv("DIRECT_CI_MODE") == "1",

		ObjectStoreType:           os.Getenv("PACKETS_OBJECT_STORE"),
		ObjectStoreBucket:         os.Getenv("PACKETS_S3_BUCKET"),
		ObjectStoreRegion:         os.Getenv("PACKETS_S3_REGION"),
		ObjectStoreEndpoint:       os.Getenv("PACKETS_S3_ENDPOINT"),
		ObjectStoreAccessKey:      os.Getenv("PACKETS_S3_ACCESS_KEY_ID"),
		ObjectStoreSecretKey:      os.Getenv("PACKETS_S3_SECRET_ACCESS_KEY"),
		ObjectStoreForcePathStyle: forcePathStyle,

		MaxWorkspaceSizeMB: maxWS,
		MaxArtifactSizeMB:  maxArt,
		WorkspaceTempDir:   os.Getenv("PACKETS_WORKSPACE_TEMP_DIR"),

		DefaultRunner: os.Getenv("PACKETS_DEFAULT_RUNNER"),

		GitHubToken:        os.Getenv("PACKETS_GITHUB_TOKEN"),
		GitHubWorkflowRepo: os.Getenv("PACKETS_GITHUB_WORKFLOW_REPO"),

		TLSEnabled:               tlsEnabled,
		TLSCertFile:              os.Getenv("PACKETS_TLS_CERT_FILE"),
		TLSKeyFile:               os.Getenv("PACKETS_TLS_KEY_FILE"),
		TLSCAFile:                os.Getenv("PACKETS_TLS_CA_FILE"),
		TLSInsecureSkipVerify:    tlsInsecure,
		MaxConcurrentJobsPerUser: maxConc,
		MaxSubmissionsPerMinute:  maxRate,
	}

	if cfg.SchedulerGRPCPort == "" {
		cfg.SchedulerGRPCPort = "50051"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.DefaultRunner == "" {
		cfg.DefaultRunner = "docker"
	}
	if cfg.WorkspaceTempDir == "" {
		cfg.WorkspaceTempDir = os.TempDir()
	}

	return cfg, nil
}

func (c *Config) ValidateForDaemon() error {
	if c.SQLiteDBPath == "" {
		return fmt.Errorf("SQLITE_DB_PATH is required")
	}
	if c.ObjectStoreType != "" && c.ObjectStoreBucket == "" {
		return fmt.Errorf("PACKETS_S3_BUCKET is required when PACKETS_OBJECT_STORE is set")
	}
	if (c.TLSCertFile != "" && c.TLSKeyFile == "") || (c.TLSCertFile == "" && c.TLSKeyFile != "") {
		return fmt.Errorf("both PACKETS_TLS_CERT_FILE and PACKETS_TLS_KEY_FILE must be provided for TLS")
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
