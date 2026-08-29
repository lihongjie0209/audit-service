package observability

import (
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

type Metrics struct {
	enabled          bool
	registry         *prometheus.Registry
	HTTPRequests     *prometheus.CounterVec
	HTTPDuration     *prometheus.HistogramVec
	CronRuns         *prometheus.CounterVec
	CronDuration     *prometheus.HistogramVec
	GRPCRequests     *prometheus.CounterVec
	GRPCDuration     *prometheus.HistogramVec
	OutboundRequests *prometheus.CounterVec
	OutboundDuration *prometheus.HistogramVec
}

func NewMetrics(cfg config.Config, db *sqlx.DB, client *redis.Client) *Metrics {
	registry := prometheus.NewRegistry()
	metrics := &Metrics{enabled: cfg.Observability.MetricsEnabled, registry: registry,
		HTTPRequests:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "http_requests_total", Help: "Total HTTP requests."}, []string{"method", "route", "status"}),
		HTTPDuration:     prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "http_request_duration_seconds", Help: "HTTP request latency.", Buckets: prometheus.DefBuckets}, []string{"method", "route"}),
		CronRuns:         prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cron_runs_total", Help: "Total cron executions."}, []string{"job", "status"}),
		CronDuration:     prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "cron_run_duration_seconds", Help: "Cron execution latency.", Buckets: prometheus.DefBuckets}, []string{"job"}),
		GRPCRequests:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "grpc_server_requests_total", Help: "Total gRPC requests."}, []string{"method", "code"}),
		GRPCDuration:     prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "grpc_server_duration_seconds", Help: "gRPC request latency.", Buckets: prometheus.DefBuckets}, []string{"method"}),
		OutboundRequests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "outbound_requests_total", Help: "Total outbound requests."}, []string{"protocol", "client", "status"}),
		OutboundDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "outbound_request_duration_seconds", Help: "Outbound request latency.", Buckets: prometheus.DefBuckets}, []string{"protocol", "client"}),
	}
	registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}), metrics.HTTPRequests, metrics.HTTPDuration, metrics.CronRuns, metrics.CronDuration, metrics.GRPCRequests, metrics.GRPCDuration, metrics.OutboundRequests, metrics.OutboundDuration)
	if db != nil {
		registry.MustRegister(collectors.NewDBStatsCollector(db.DB, "primary"))
	}
	if client != nil {
		registerRedisMetrics(registry, client)
	}
	return metrics
}

func (m *Metrics) Enabled() bool { return m.enabled }
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
func (m *Metrics) ObserveCron(job, status string, started time.Time) {
	if !m.enabled {
		return
	}
	m.CronRuns.WithLabelValues(job, status).Inc()
	m.CronDuration.WithLabelValues(job).Observe(time.Since(started).Seconds())
}

func registerRedisMetrics(registry *prometheus.Registry, client *redis.Client) {
	metrics := []struct {
		name, help string
		value      func() float64
	}{
		{"redis_pool_hits_total", "Redis pool hits.", func() float64 { return float64(client.PoolStats().Hits) }},
		{"redis_pool_misses_total", "Redis pool misses.", func() float64 { return float64(client.PoolStats().Misses) }},
		{"redis_pool_timeouts_total", "Redis pool timeouts.", func() float64 { return float64(client.PoolStats().Timeouts) }},
		{"redis_pool_connections", "Current Redis connections.", func() float64 { return float64(client.PoolStats().TotalConns) }},
		{"redis_pool_idle_connections", "Current idle Redis connections.", func() float64 { return float64(client.PoolStats().IdleConns) }},
	}
	for _, metric := range metrics {
		registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: metric.name, Help: metric.help}, metric.value))
	}
}
