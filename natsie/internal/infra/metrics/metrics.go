// Package metrics holds the bot's Prometheus instrumentation.
//
// A Metrics owns its own registry (not the global default) so the collectors
// are explicit and the package stays testable. Every Observe* method is safe
// to call on a nil *Metrics, so callers in serve/httpsrv can record
// unconditionally without guarding on whether metrics are enabled.
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "natsie"

// Metrics is the bot's metric set plus the registry that backs /metrics.
type Metrics struct {
	reg *prometheus.Registry

	scanDuration    *prometheus.HistogramVec
	scanRuns        *prometheus.CounterVec
	scanCandidates  *prometheus.GaugeVec
	applyConsumers  *prometheus.CounterVec
	approvalLatency prometheus.Histogram
}

// New builds the metric set, registers it (along with the standard Go and
// process collectors and a build-info gauge), and returns it ready to serve.
func New(version string) *Metrics {
	m := &Metrics{
		reg: prometheus.NewRegistry(),
		scanDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Subsystem: "scan", Name: "duration_seconds",
			Help:    "Wall-clock duration of a scheduled scan run.",
			Buckets: prometheus.DefBuckets,
		}, []string{"schedule", "kind"}),
		scanRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "scan", Name: "runs_total",
			Help: "Scheduled scan runs by outcome (success or error).",
		}, []string{"schedule", "kind", "outcome"}),
		scanCandidates: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace, Subsystem: "scan", Name: "candidates",
			Help: "Candidates found by the most recent scan (stale consumers or unlimited streams).",
		}, []string{"schedule", "kind"}),
		applyConsumers: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "apply", Name: "consumers_total",
			Help: "Consumers processed at apply time by outcome.",
		}, []string{"outcome"}),
		approvalLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace, Subsystem: "approval", Name: "latency_seconds",
			Help: "Time between manifest generation and approval apply.",
			// 1m, 4m, 16m, ... ~4.5d — approvals span minutes to days.
			Buckets: prometheus.ExponentialBuckets(60, 4, 8),
		}),
	}

	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace, Name: "build_info",
		Help: "Build information; the value is always 1.",
	}, []string{"version"})
	buildInfo.WithLabelValues(version).Set(1)

	m.reg.MustRegister(
		m.scanDuration,
		m.scanRuns,
		m.scanCandidates,
		m.applyConsumers,
		m.approvalLatency,
		buildInfo,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return m
}

// Handler serves the registry in Prometheus text exposition format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// ObserveScan records the duration, outcome, and candidate count of one
// scheduled scan run. candidates is recorded only on success; an errored run
// leaves the previous gauge value untouched rather than reporting a misleading
// zero.
func (m *Metrics) ObserveScan(schedule, kind string, candidates int, dur time.Duration, err error) {
	if m == nil {
		return
	}

	m.scanDuration.WithLabelValues(schedule, kind).Observe(dur.Seconds())

	outcome := "success"
	if err != nil {
		outcome = "error"
	}

	m.scanRuns.WithLabelValues(schedule, kind, outcome).Inc()

	if err == nil {
		m.scanCandidates.WithLabelValues(schedule, kind).Set(float64(candidates))
	}
}

// ObserveApply records the per-consumer outcomes of an approval apply and, when
// age is positive, the time the manifest waited between generation and apply.
func (m *Metrics) ObserveApply(deleted, preserved, gone, skipped, failed int, age time.Duration) {
	if m == nil {
		return
	}

	m.applyConsumers.WithLabelValues("deleted").Add(float64(deleted))
	m.applyConsumers.WithLabelValues("preserved").Add(float64(preserved))
	m.applyConsumers.WithLabelValues("gone").Add(float64(gone))
	m.applyConsumers.WithLabelValues("skipped").Add(float64(skipped))
	m.applyConsumers.WithLabelValues("failed").Add(float64(failed))

	if age > 0 {
		m.approvalLatency.Observe(age.Seconds())
	}
}
