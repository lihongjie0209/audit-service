package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"

	auditdomain "github.com/lihongjie0209/audit-service/internal/audit"
	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/lihongjie0209/audit-service/internal/principal"
	"github.com/lihongjie0209/microservice-platform-go/eventbus"
	commonv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/common/v1"
	"go.uber.org/fx"
)

type auditEventRuntime struct {
	config  config.Config
	service *auditdomain.Service
	logger  *slog.Logger
	bus     *eventbus.Bus
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func newAuditEventRuntime(lifecycle fx.Lifecycle, cfg config.Config, service *auditdomain.Service, logger *slog.Logger) *auditEventRuntime {
	runtime := &auditEventRuntime{config: cfg, service: service, logger: logger}
	lifecycle.Append(fx.Hook{OnStart: runtime.start, OnStop: runtime.stop})
	return runtime
}

func (r *auditEventRuntime) start(ctx context.Context) error {
	if !r.config.EventBus.Enabled {
		return nil
	}
	bus, err := eventbus.New(ctx, eventbus.Config{URLs: r.config.EventBus.URLs, ClientName: r.config.App.Name, StreamName: r.config.EventBus.StreamName, Subjects: []string{"platform.>"}, Storage: r.config.EventBus.Storage, MaxAge: r.config.EventBus.MaxAge, DuplicateWindow: r.config.EventBus.DuplicateWindow, ConnectTimeout: r.config.EventBus.ConnectTimeout, PublishTimeout: r.config.EventBus.PublishTimeout, ConsumerAckWait: r.config.EventBus.ConsumerAckWait, ConsumerMaxDeliver: r.config.EventBus.ConsumerMaxDeliver})
	if err != nil {
		return err
	}
	r.bus = bus
	runCtx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		if err := bus.Consume(runCtx, "audit-service-all-events", "platform.>", r.consume); err != nil && !errors.Is(err, context.Canceled) {
			r.logger.ErrorContext(runCtx, "audit event consumer stopped", "error", err)
		}
	}()
	return nil
}

func (r *auditEventRuntime) consume(ctx context.Context, envelope *commonv1.EventEnvelope) error {
	if envelope == nil {
		return errors.New("event envelope is required")
	}
	summary, err := json.Marshal(map[string]any{"event_type": envelope.GetEventType(), "schema_version": envelope.GetSchemaVersion()})
	if err != nil {
		return err
	}
	requestContext := envelope.GetContext()
	record := auditdomain.Record{ID: envelope.GetEventId(), TenantID: envelope.GetTenantId(), Action: envelope.GetEventType(), ResourceType: envelope.GetAggregateType(), ResourceID: envelope.GetAggregateId(), SourceService: sourceService(envelope.GetEventType()), AfterSummary: summary}
	if envelope.GetOccurredAt() != nil {
		record.OccurredAt = envelope.GetOccurredAt().AsTime()
	}
	if requestContext != nil {
		record.ActorID, record.ActorType, record.RequestID, record.TraceID = requestContext.GetActorId(), requestContext.GetActorType(), requestContext.GetRequestId(), requestContext.GetTraceId()
	}
	_, err = r.service.Record(principal.WithContext(ctx, principal.Principal{Subject: "audit-event-consumer", Method: principal.AuthenticationPSK}), record)
	return err
}

func sourceService(eventType string) string {
	parts := strings.Split(eventType, ".")
	if len(parts) > 1 && parts[0] == "platform" {
		return parts[1] + "-service"
	}
	return "unknown"
}

func (r *auditEventRuntime) stop(context.Context) error {
	if r.cancel != nil {
		r.cancel()
		r.wg.Wait()
	}
	if r.bus != nil {
		return r.bus.Close()
	}
	return nil
}

var AuditEventBusModule = fx.Module("audit-event-bus", fx.Provide(newAuditEventRuntime), fx.Invoke(func(*auditEventRuntime) {}))
