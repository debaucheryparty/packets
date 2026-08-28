package scheduler

import (
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// TailscaleInterceptor ensures the caller is verified via Tailscale whois
func TailscaleInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		if err := verifyTailscaleIdentity(ctx); err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "tailscale auth failed: %v", err)
		}
		return handler(ctx, req)
	}
}

// TailscaleStreamInterceptor ensures the caller is verified via Tailscale whois for streams
func TailscaleStreamInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if err := verifyTailscaleIdentity(ss.Context()); err != nil {
			return status.Errorf(codes.Unauthenticated, "tailscale auth failed: %v", err)
		}
		return handler(srv, ss)
	}
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
