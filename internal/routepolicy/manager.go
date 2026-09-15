package routepolicy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/lihongjie0209/audit-service/internal/observability"
	"github.com/lihongjie0209/microservice-platform-go/authz"
	platformpolicy "github.com/lihongjie0209/microservice-platform-go/routepolicy"
	"github.com/redis/go-redis/v9"
)

type Manager struct {
	repository *Repository
	snapshot   *platformpolicy.Snapshot
	redis      *redis.Client
	channel    string
	interval   time.Duration
	logger     *slog.Logger
	metrics    *observability.Metrics
	revision   atomic.Int64
	refresh    sync.Mutex
	routeIDs   sync.Map
}

func NewManager(repository *Repository, compiler *platformpolicy.Compiler, client *redis.Client, cfg config.Config, logger *slog.Logger, metrics *observability.Metrics) *Manager {
	return &Manager{repository: repository, snapshot: platformpolicy.NewSnapshot(compiler), redis: client, channel: cfg.Runtime.ActiveProfile + ":" + cfg.App.Name + ":route-policy:changed", interval: cfg.Authorization.PolicyRefreshInterval, logger: logger, metrics: metrics}
}
func (m *Manager) Refresh(ctx context.Context) error {
	return m.RefreshSource(ctx, "manual")
}
func (m *Manager) RefreshSource(ctx context.Context, source string) (resultErr error) {
	started := time.Now()
	defer func() {
		status := "success"
		if resultErr != nil {
			status = "failure"
		}
		if m.metrics != nil {
			m.metrics.ObserveRoutePolicyRefresh(source, status, started)
		}
	}()
	m.refresh.Lock()
	defer m.refresh.Unlock()
	if err := m.snapshot.Reload(ctx, m.repository); err != nil {
		return err
	}
	revision, err := m.repository.Revision(ctx)
	if err == nil {
		m.revision.Store(revision.UnixNano())
	}
	return err
}
func (m *Manager) ValidateRoutes(ctx context.Context, service string) error {
	ids, err := m.repository.ActiveRouteIDs(ctx, service)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := m.snapshot.Evaluate(ctx, id, nil); err != nil && !errors.Is(err, platformpolicy.ErrDenied) {
			return err
		}
	}
	return nil
}
func (m *Manager) EvaluateRoute(ctx context.Context, protocol, method, path, service string, authorizer authz.Authorizer) error {
	key := protocol + "\x00" + method + "\x00" + path + "\x00" + service
	id, ok := m.routeIDs.Load(key)
	if !ok {
		route, err := platformpolicy.NewRoute(protocol, method, path, service, "")
		if err != nil {
			return err
		}
		id = route.ID
		m.routeIDs.Store(key, id)
	}
	return m.snapshot.Evaluate(ctx, id.(string), authorizer)
}
func (m *Manager) Notify(ctx context.Context) error {
	if m.redis == nil {
		return nil
	}
	if err := m.redis.Publish(ctx, m.channel, "refresh").Err(); err != nil {
		return fmt.Errorf("notify route policy: %w", err)
	}
	return nil
}
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	var messages <-chan *redis.Message
	if m.redis != nil {
		sub := m.redis.Subscribe(ctx, m.channel)
		defer func() { _ = sub.Close() }()
		messages = sub.Channel()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-messages:
			m.reload(ctx, "redis")
		case <-ticker.C:
			revision, err := m.repository.Revision(ctx)
			if err != nil {
				m.logger.Warn("check route policy revision", "error", err)
				continue
			}
			if revision.UnixNano() != m.revision.Load() {
				m.reload(ctx, "poll")
			}
		}
	}
}
func (m *Manager) reload(ctx context.Context, source string) {
	if err := m.RefreshSource(ctx, source); err != nil && !errors.Is(err, context.Canceled) {
		m.logger.Error("refresh route policies", "error", err)
	}
}
