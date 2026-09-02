package idempotency

import (
	"github.com/lihongjie0209/audit-service/internal/config"
	platformidempotency "github.com/lihongjie0209/microservice-platform-go/idempotency"
	"github.com/redis/go-redis/v9"
)

type State = platformidempotency.State
type Failure = platformidempotency.Failure
type Decision = platformidempotency.Decision
type Manager = platformidempotency.Manager

const (
	StateAcquired   = platformidempotency.StateAcquired
	StateProcessing = platformidempotency.StateProcessing
	StateCompleted  = platformidempotency.StateCompleted
	StateFailed     = platformidempotency.StateFailed
	StateConflict   = platformidempotency.StateConflict
)

func New(client *redis.Client, cfg config.Config) *Manager {
	return platformidempotency.New(client, platformidempotency.Config{Enabled: cfg.Idempotency.Enabled, Service: cfg.App.Name, ProcessingTTL: cfg.Idempotency.ProcessingTTL, ResultTTL: cfg.Idempotency.ResultTTL, FailureTTL: cfg.Idempotency.FailureTTL})
}
