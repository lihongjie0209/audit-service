package observability

import (
	"context"
	"fmt"

	"github.com/lihongjie0209/audit-service/internal/buildinfo"
	"github.com/lihongjie0209/audit-service/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/fx"
)

type Tracing struct {
	enabled  bool
	provider *sdktrace.TracerProvider
}

func (t *Tracing) Enabled() bool { return t.enabled }

func NewTracing(lc fx.Lifecycle, cfg config.Config) (*Tracing, error) {
	tracing := &Tracing{enabled: cfg.Observability.TracingEnabled}
	if !tracing.enabled {
		return tracing, nil
	}
	exporter, err := otlptracehttp.New(context.Background(), otlptracehttp.WithEndpointURL(cfg.Observability.TracingEndpoint))
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}
	res := resource.NewSchemaless(attribute.String("service.name", cfg.App.Name), attribute.String("service.version", buildinfo.Version))
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res), sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.Observability.TracingSampleRatio))))
	tracing.provider = provider
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	lc.Append(fx.StopHook(provider.Shutdown))
	return tracing, nil
}
