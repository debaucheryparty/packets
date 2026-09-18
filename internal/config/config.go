package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	OracleVMTailscaleHost string
	SchedulerGRPCPort     string
	SQLiteDBPath          string
	GitHubActionsToken    string
	GitHubActionsRepo     string
	CircleCIToken         string
	CircleCIProjectSlug   string
	LogLevel              string

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

	TLSEnabled               bool
	TLSCertFile              string
	TLSKeyFile               string
	TLSCAFile                string
	TLSInsecureSkipVerify    bool
	MaxConcurrentJobsPerUser int
	MaxSubmissionsPerMinute  int
	AuthToken                string
}

func LoadConfig(ctx context.Context) (*Config, error) {
	home, _ := os.UserHomeDir()
	globalConfig := ""
	if home != "" {
		globalConfig = filepath.Join(home, ".packets", "config.env")
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

	serverHost := os.Getenv("PACKETS_SERVER_ADDR")
	if serverHost == "" {
		serverHost = os.Getenv("ORACLE_VM_TAILSCALE_HOSTNAME")
	}

	cfg := &Config{
		OracleVMTailscaleHost: serverHost,
		SchedulerGRPCPort:     os.Getenv("SCHEDULER_GRPC_PORT"),
		SQLiteDBPath:          os.Getenv("SQLITE_DB_PATH"),
		GitHubActionsToken:    os.Getenv("GITHUB_ACTIONS_TOKEN"),
		GitHubActionsRepo:     os.Getenv("GITHUB_ACTIONS_REPO"),
		CircleCIToken:         os.Getenv("CIRCLECI_TOKEN"),
		CircleCIProjectSlug:   os.Getenv("CIRCLECI_PROJECT_SLUG"),
		LogLevel:              os.Getenv("LOG_LEVEL"),

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

		TLSEnabled:               tlsEnabled,
		TLSCertFile:              os.Getenv("PACKETS_TLS_CERT_FILE"),
		TLSKeyFile:               os.Getenv("PACKETS_TLS_KEY_FILE"),
		TLSCAFile:                os.Getenv("PACKETS_TLS_CA_FILE"),
		TLSInsecureSkipVerify:    tlsInsecure,
		MaxConcurrentJobsPerUser: maxConc,
		MaxSubmissionsPerMinute:  maxRate,
		AuthToken:                os.Getenv("PACKETS_AUTH_TOKEN"),
	}

	prof, _ := LoadProfile()
	if prof != nil {
		if cfg.AuthToken == "" {
			cfg.AuthToken = prof.AuthToken
		}
		if cfg.OracleVMTailscaleHost == "" && prof.ServerAddr != "" {
			cfg.OracleVMTailscaleHost = prof.ServerAddr
		}
		if !cfg.TLSEnabled && prof.TLSEnabled {
			cfg.TLSEnabled = true
			if cfg.TLSCertFile == "" {
				cfg.TLSCertFile = prof.TLSCertFile
			}
			if cfg.TLSKeyFile == "" {
				cfg.TLSKeyFile = prof.TLSKeyFile
			}
			if cfg.TLSCAFile == "" {
				cfg.TLSCAFile = prof.TLSCAFile
			}
			cfg.TLSInsecureSkipVerify = prof.TLSInsecureSkipVerify
		}
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
	if cfg.SQLiteDBPath == "" {
		cfg.SQLiteDBPath = "packets.db"
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
