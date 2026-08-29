package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type Envelope struct {
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	AggregateID   string          `json:"aggregate_id"`
	TenantID      string          `json:"tenant_id,omitempty"`
	SchemaVersion int             `json:"schema_version"`
	OccurredAt    string          `json:"occurred_at"`
	RequestID     string          `json:"request_id,omitempty"`
	TraceID       string          `json:"trace_id,omitempty"`
	ActorID       string          `json:"actor_id,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

type Handler func(context.Context, Envelope) error

type Bus struct {
	cfg    config.EventBus
	conn   *nats.Conn
	js     jetstream.JetStream
	stream jetstream.Stream
	mu     sync.Mutex
	closed bool
}

func New(ctx context.Context, cfg config.Config) (*Bus, error) {
	if !cfg.EventBus.Enabled {
		return nil, nil
	}
	eventCfg := cfg.EventBus
	conn, err := nats.Connect(strings.Join(eventCfg.URLs, ","), nats.Name(cfg.App.Name), nats.Timeout(eventCfg.ConnectTimeout), nats.MaxReconnects(-1), nats.ReconnectWait(eventCfg.ReconnectWait))
	if err != nil {
		return nil, fmt.Errorf("connect NATS: %w", err)
	}
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create JetStream: %w", err)
	}
	storage := jetstream.FileStorage
	if eventCfg.Storage == "memory" {
		storage = jetstream.MemoryStorage
	}
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{Name: eventCfg.StreamName, Subjects: eventCfg.Subjects, Storage: storage, Retention: jetstream.LimitsPolicy, MaxAge: eventCfg.MaxAge, Duplicates: eventCfg.DuplicateWindow})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("provision JetStream: %w", err)
	}
	return &Bus{cfg: eventCfg, conn: conn, js: js, stream: stream}, nil
}

func (b *Bus) Publish(ctx context.Context, subject string, envelope Envelope) error {
	if b == nil {
		return errors.New("event bus is disabled")
	}
	if subject == "" || envelope.EventID == "" || envelope.EventType == "" || envelope.SchemaVersion < 1 {
		return errors.New("subject and valid event envelope are required")
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	publishCtx, cancel := context.WithTimeout(ctx, b.cfg.PublishTimeout)
	defer cancel()
	message := nats.NewMsg(subject)
	message.Data = payload
	message.Header.Set("X-Request-ID", envelope.RequestID)
	message.Header.Set("Traceparent", envelope.TraceID)
	if _, err := b.js.PublishMsg(publishCtx, message, jetstream.WithMsgID(envelope.EventID)); err != nil {
		return fmt.Errorf("publish event: %w", err)
	}
	return nil
}

func (b *Bus) Consume(ctx context.Context, durable, filter string, handler Handler) error {
	if b == nil || durable == "" || filter == "" || handler == nil {
		return errors.New("enabled event bus, durable, filter, and handler are required")
	}
	consumer, err := b.stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{Durable: durable, FilterSubject: filter, AckPolicy: jetstream.AckExplicitPolicy, AckWait: b.cfg.ConsumerAckWait, MaxDeliver: b.cfg.ConsumerMaxDeliver})
	if err != nil {
		return fmt.Errorf("provision consumer: %w", err)
	}
	runner, err := consumer.Consume(func(message jetstream.Msg) {
		var envelope Envelope
		if err := json.Unmarshal(message.Data(), &envelope); err != nil {
			_ = message.Term()
			return
		}
		if err := handler(ctx, envelope); err != nil {
			_ = message.Nak()
			return
		}
		_ = message.Ack()
	})
	if err != nil {
		return fmt.Errorf("start consumer: %w", err)
	}
	defer runner.Stop()
	<-ctx.Done()
	return ctx.Err()
}

func (b *Bus) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	if err := b.conn.Drain(); err != nil {
		b.conn.Close()
		return fmt.Errorf("drain NATS: %w", err)
	}
	b.conn.Close()
	return nil
}
