package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/debaucheryparty/packets/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type bearerTokenAuth struct {
	token string
}

func (b bearerTokenAuth) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{
		"authorization": "Bearer " + b.token,
	}, nil
}

func (b bearerTokenAuth) RequireTransportSecurity() bool {
	return false
}

func DialScheduler(ctx context.Context, cfg *config.Config) (*grpc.ClientConn, error) {
	host := cfg.OracleVMTailscaleHost
	var addr string
	if host == "" {
		addr = "127.0.0.1" + cfg.SchedulerAddr()
	} else if strings.Contains(host, ":") {
		addr = host
	} else {
		addr = host + cfg.SchedulerAddr()
	}

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

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(transportCreds),
		grpc.WithBlock(),
	}

	if cfg.AuthToken != "" {
		opts = append(opts, grpc.WithPerRPCCredentials(bearerTokenAuth{token: cfg.AuthToken}))
	}

	conn, err := grpc.DialContext(dialCtx, addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial scheduler (%s): %w", addr, err)
	}

	return conn, nil
}
