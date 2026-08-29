package observability

import (
	"testing"
	"time"

	"github.com/lihongjie0209/audit-service/internal/config"
)

func TestMetrics_Collect(t *testing.T) {
	t.Parallel()
	metrics := NewMetrics(config.Config{Observability: config.Observability{MetricsEnabled: true}}, nil, nil)
	metrics.HTTPRequests.WithLabelValues("POST", "/test", "200").Inc()
	metrics.HTTPDuration.WithLabelValues("POST", "/test").Observe(0.01)
	metrics.ObserveCron("sample", "success", time.Now())
	families, err := metrics.registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"http_requests_total": false, "http_request_duration_seconds": false, "cron_runs_total": false}
	for _, family := range families {
		if _, ok := want[family.GetName()]; ok {
			want[family.GetName()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("metric %q not gathered", name)
		}
	}
}
