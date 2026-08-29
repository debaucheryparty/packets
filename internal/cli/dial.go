package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/waris4ly/packets/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func DialScheduler(ctx context.Context, cfg *config.Config) (*grpc.ClientConn, error) {
	addr := cfg.OracleVMTailscaleHost + cfg.SchedulerAddr()

	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var transportCreds credentials.TransportCredentials

	if cfg.TLSEnabled || cfg.TLSCAFile != "" || cfg.TLSCertFile != "" {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: cfg.TLSInsecureSkipVerify,
		}

		if cfg.TLSCAFile != "" {
			caCert, err := os.ReadFile(cfg.TLSCAFile)
			if err != nil {
				return nil, fmt.Errorf("read TLS CA file: %w", err)
			}
			caPool := x509.NewCertPool()
			if !caPool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("failed to append CA certificate")
			}
			tlsConfig.RootCAs = caPool
		}

		if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
			clientCert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
			if err != nil {
				return nil, fmt.Errorf("load client TLS certificate: %w", err)
			}
			tlsConfig.Certificates = []tls.Certificate{clientCert}
		}

		transportCreds = credentials.NewTLS(tlsConfig)
	} else {
		transportCreds = insecure.NewCredentials()
	}

	conn, err := grpc.DialContext(dialCtx, addr, grpc.WithTransportCredentials(transportCreds), grpc.WithBlock())
	if err != nil {
		return nil, fmt.Errorf("dial scheduler (%s): %w", addr, err)
	}

	return conn, nil
}
