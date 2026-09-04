package shim

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/debaucheryparty/packets/pkg/apitypes"
)

type RouteTarget struct {
	Backend apitypes.Backend
	Config  map[string]string
}

type Router struct {
	logger   *slog.Logger
	detector *Detector
}

func NewRouter(logger *slog.Logger, detector *Detector) *Router {
	return &Router{
		logger:   logger,
		detector: detector,
	}
}

func (r *Router) Route(ctx context.Context, dir string) (RouteTarget, error) {
	def, err := r.detector.DetectToolchain(dir)
	if err != nil {
		return RouteTarget{}, fmt.Errorf("Router.Route: %w", err)
	}

	r.logger.InfoContext(ctx, "toolchain detected",
		slog.String("toolchain", string(def.Name)),
		slog.String("backend", def.Backend.String()),
	)

	return RouteTarget{
		Backend: def.Backend,
	}, nil
}
