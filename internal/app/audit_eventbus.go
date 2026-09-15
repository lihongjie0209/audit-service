package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	auditdomain "github.com/lihongjie0209/audit-service/internal/audit"
	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/lihongjie0209/microservice-platform-go/eventbus"
	"github.com/lihongjie0209/microservice-platform-go/operationlog"
	platformprincipal "github.com/lihongjie0209/microservice-platform-go/principal"
	"github.com/lihongjie0209/microservice-platform-go/redact"
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
	summary, err := summaryFromEnvelope(envelope)
	if err != nil {
		return err
	}
	requestContext := envelope.GetContext()
	record := auditdomain.Record{ID: envelope.GetEventId(), TenantID: envelope.GetTenantId(), ApplicationID: envelope.GetApplicationId(), Action: envelope.GetEventType(), ResourceType: envelope.GetAggregateType(), ResourceID: envelope.GetAggregateId(), SourceService: sourceService(envelope), AfterSummary: summary}
	if envelope.GetOccurredAt() != nil {
		record.OccurredAt = envelope.GetOccurredAt().AsTime()
	}
	if requestContext != nil {
		record.ActorID, record.ActorType, record.RequestID, record.TraceID = requestContext.GetActorId(), requestContext.GetActorType(), requestContext.GetRequestId(), requestContext.GetTraceId()
	}
	_, err = r.service.Record(platformprincipal.SystemContext(ctx, "audit-event-consumer"), record)
	return err
}

func summaryFromEnvelope(envelope *commonv1.EventEnvelope) ([]byte, error) {
	if envelope == nil {
		return nil, errors.New("event envelope is required")
	}
	value := map[string]any{"event_type": envelope.GetEventType(), "schema_version": envelope.GetSchemaVersion()}
	if payload := envelope.GetPayload(); len(payload) > 0 && json.Valid(payload) {
		redacted, err := redact.JSON(payload)
		if err != nil {
			return nil, fmt.Errorf("redact event payload: %w", err)
		}
		value["payload"] = json.RawMessage(redacted)
	}
	return json.Marshal(value)
}

func sourceService(envelope *commonv1.EventEnvelope) string {
	if payload := envelope.GetPayload(); json.Valid(payload) {
		var metadata struct {
			Source string `json:"source"`
		}
		if json.Unmarshal(payload, &metadata) == nil && strings.TrimSpace(metadata.Source) != "" {
			return strings.TrimSpace(metadata.Source)
		}
	}
	parts := strings.Split(envelope.GetEventType(), ".")
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

func (r *auditEventRuntime) Publish(ctx context.Context, subject string, envelope *commonv1.EventEnvelope) error {
	if r == nil || r.bus == nil {
		return errors.New("audit event bus is unavailable")
	}
	return r.bus.Publish(ctx, subject, envelope)
}

func newOperationLogRecorder(cfg config.Config, publisher *auditEventRuntime) (operationlog.Recorder, error) {
	return operationlog.New(operationlog.Config{Enabled: cfg.OperationLog.Enabled, Subject: cfg.OperationLog.Subject, MaxPayloadBytes: cfg.OperationLog.MaxPayloadBytes}, publisher)
}

var AuditEventBusModule = fx.Module("audit-event-bus", fx.Provide(newAuditEventRuntime, newOperationLogRecorder), fx.Invoke(func(*auditEventRuntime) {}))
