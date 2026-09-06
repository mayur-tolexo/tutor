package httpapi

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// metrics are the operational signals the design calls for: run volume and
// latency by outcome, capacity refusals, and where hints come from.
type metrics struct {
	reg         *prometheus.Registry
	attempts    *prometheus.CounterVec
	runLatency  *prometheus.HistogramVec
	poolBusy    prometheus.Counter
	hints       *prometheus.CounterVec
	modelTokens prometheus.Counter
	rateLimited *prometheus.CounterVec
}

// newMetrics registers the collectors on a private registry so the endpoint
// only exposes what the service deliberately emits.
func newMetrics() *metrics {
	m := &metrics{
		reg: prometheus.NewRegistry(),
		attempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tutor_attempts_total", Help: "Attempts by mode and outcome.",
		}, []string{"mode", "outcome"}),
		runLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "tutor_run_seconds", Help: "Sandbox run wall clock by mode.",
			Buckets: []float64{0.25, 0.5, 1, 2, 3, 5, 8, 13},
		}, []string{"mode"}),
		poolBusy: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tutor_pool_busy_total", Help: "Attempts refused because no sandbox capacity was available.",
		}),
		hints: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tutor_hints_total", Help: "Hints served by source and language.",
		}, []string{"source", "lang"}),
		modelTokens: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tutor_model_tokens_total", Help: "Tokens consumed by model-generated hints.",
		}),
		rateLimited: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tutor_rate_limited_total", Help: "Requests refused by a rate limit, by limit name.",
		}, []string{"limit"}),
	}
	m.reg.MustRegister(m.attempts, m.runLatency, m.poolBusy, m.hints, m.modelTokens, m.rateLimited)
	return m
}

// handler exposes the registry in Prometheus text format.
func (m *metrics) handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}
