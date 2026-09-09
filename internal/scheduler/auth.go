package scheduler

import (
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// AuthInterceptor checks bearer token if requiredToken is set; otherwise falls back to Tailscale identity.
func AuthInterceptor(requiredToken string) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		if requiredToken != "" {
			if err := verifyBearerToken(ctx, requiredToken); err != nil {
				return nil, status.Errorf(codes.Unauthenticated, "authentication failed: %v", err)
			}
			return handler(ctx, req)
		}

		if err := verifyTailscaleIdentity(ctx); err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "tailscale auth failed: %v", err)
		}
		return handler(ctx, req)
	}
}

// AuthStreamInterceptor checks bearer token if requiredToken is set; otherwise falls back to Tailscale identity.
func AuthStreamInterceptor(requiredToken string) grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if requiredToken != "" {
			if err := verifyBearerToken(ss.Context(), requiredToken); err != nil {
				return status.Errorf(codes.Unauthenticated, "authentication failed: %v", err)
			}
			return handler(srv, ss)
		}

		if err := verifyTailscaleIdentity(ss.Context()); err != nil {
			return status.Errorf(codes.Unauthenticated, "tailscale auth failed: %v", err)
		}
		return handler(srv, ss)
	}
}

// TailscaleInterceptor ensures the caller is verified via Tailscale whois
func TailscaleInterceptor() grpc.UnaryServerInterceptor {
	return AuthInterceptor("")
}

// TailscaleStreamInterceptor ensures the caller is verified via Tailscale whois for streams
func TailscaleStreamInterceptor() grpc.StreamServerInterceptor {
	return AuthStreamInterceptor("")
}

func verifyBearerToken(ctx context.Context, requiredToken string) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return fmt.Errorf("missing incoming metadata")
	}

	authHeaders := md.Get("authorization")
	if len(authHeaders) == 0 {
		return fmt.Errorf("missing authorization header")
	}

	token := strings.TrimPrefix(authHeaders[0], "Bearer ")
	token = strings.TrimSpace(token)
	if token != requiredToken {
		return fmt.Errorf("invalid bearer token")
	}
	return nil
}

func verifyTailscaleIdentity(ctx context.Context) error {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return fmt.Errorf("no peer in context")
	}

	addr := p.Addr.String()
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		addr = addr[:idx]
	}

	addr = strings.Trim(addr, "[]")

	if _, err := netip.ParseAddr(addr); err != nil {
		return fmt.Errorf("invalid peer address: %s", addr)
	}

	if addr == "127.0.0.1" || addr == "::1" {
		return nil
	}

	cmd := exec.CommandContext(ctx, "tailscale", "whois", addr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tailscale whois failed for %s: %w\noutput: %s", addr, err, string(output))
	}

	return nil
}
