package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	TransactionsTotal    *prometheus.CounterVec
	DuplicatesTotal      prometheus.Counter
	RetriesTotal         prometheus.Counter
	DLQTotal             prometheus.Counter
	ConcurrencyConflicts prometheus.Counter
	OutboxPendingCount   prometheus.Gauge
	OutboxPublishLatency prometheus.Histogram
	ProcessingDuration   prometheus.Histogram
	ReconciliationDiv    prometheus.Counter
}

func New() *Metrics {
	return &Metrics{
		TransactionsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "wager_transactions_total",
				Help: "Total number of wager transactions by status",
			},
			[]string{"status"},
		),
		DuplicatesTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "wager_transactions_duplicate_total",
				Help: "Total number of duplicate transactions received",
			},
		),
		RetriesTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "wager_transactions_retry_total",
				Help: "Total number of transaction retries",
			},
		),
		DLQTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "wager_transactions_dlq_total",
				Help: "Total number of messages sent to DLQ",
			},
		),
		ConcurrencyConflicts: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "wager_concurrency_conflict_total",
				Help: "Total number of optimistic lock conflicts",
			},
		),
		OutboxPendingCount: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "wager_outbox_pending_count",
				Help: "Number of unpublished outbox events",
			},
		),
		OutboxPublishLatency: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "wager_outbox_publish_latency_seconds",
				Help:    "Latency of outbox event publishing",
				Buckets: prometheus.DefBuckets,
			},
		),
		ProcessingDuration: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "wager_processing_duration_seconds",
				Help:    "Transaction processing duration",
				Buckets: prometheus.DefBuckets,
			},
		),
		ReconciliationDiv: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "wager_reconciliation_divergence_total",
				Help: "Total number of reconciliation divergences detected",
			},
		),
	}
}
