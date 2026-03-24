package metrics

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	RelayCreates     *prometheus.CounterVec
	RelayClaims      *prometheus.CounterVec
	RateLimitHits    *prometheus.CounterVec
	TelegramRequests *prometheus.HistogramVec
	WorkerRuns       *prometheus.CounterVec
}

func New() *Metrics {
	m := &Metrics{
		RelayCreates: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "relaybot_relay_creates_total",
				Help: "Number of relay create attempts by outcome.",
			},
			[]string{"outcome"},
		),
		RelayClaims: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "relaybot_relay_claims_total",
				Help: "Number of relay claim attempts by outcome and delivery method.",
			},
			[]string{"outcome", "method"},
		),
		RateLimitHits: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "relaybot_rate_limit_hits_total",
				Help: "Number of rate limit hits by type.",
			},
			[]string{"type"},
		),
		TelegramRequests: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "relaybot_telegram_request_seconds",
				Help:    "Latency of Telegram API requests by method and status.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "status"},
		),
		WorkerRuns: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "relaybot_worker_runs_total",
				Help: "Number of background worker runs by task and status.",
			},
			[]string{"task", "status"},
		),
	}

	prometheus.MustRegister(
		m.RelayCreates,
		m.RelayClaims,
		m.RateLimitHits,
		m.TelegramRequests,
		m.WorkerRuns,
	)

	return m
}
