package scheduler

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	JobsSubmittedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "packets_jobs_submitted_total",
		Help: "The total number of submitted build jobs",
	}, []string{"toolchain", "cache_hit"})

	JobsFailedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "packets_jobs_failed_total",
		Help: "The total number of failed build jobs",
	}, []string{"toolchain", "provider"})

	FallbackTriggeredTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "packets_fallback_triggered_total",
		Help: "The total number of times fallback execution was triggered",
	}, []string{"toolchain", "reason"})
)

func MetricsHandler() http.Handler {
	return promhttp.Handler()
}
