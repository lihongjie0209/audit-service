package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/redis/go-redis/v9"
)

type State string

const (
	StateAcquired   State = "acquired"
	StateProcessing State = "processing"
	StateCompleted  State = "completed"
	StateFailed     State = "failed"
	StateConflict   State = "conflict"
)

type Failure struct {
	Code       int    `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status"`
}
type Decision struct {
	State    State
	Response json.RawMessage
	Failure  Failure
	Owner    string
}
type Manager struct {
	client *redis.Client
	cfg    config.Idempotency
}

func New(client *redis.Client, cfg config.Config) *Manager {
	return &Manager{client: client, cfg: cfg.Idempotency}
}
func (m *Manager) Enabled() bool { return m != nil && m.cfg.Enabled }

var beginScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
  redis.call('HSET', KEYS[1], 'state', 'processing', 'fingerprint', ARGV[1], 'owner', ARGV[3])
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
  return {'acquired', '', ''}
end
local fingerprint = redis.call('HGET', KEYS[1], 'fingerprint')
if fingerprint ~= ARGV[1] then return {'conflict', '', ''} end
return {redis.call('HGET', KEYS[1], 'state') or 'processing', redis.call('HGET', KEYS[1], 'response') or '', redis.call('HGET', KEYS[1], 'failure') or ''}
`)

func (m *Manager) Begin(ctx context.Context, key, fingerprint string) (Decision, error) {
	if !m.Enabled() || m.client == nil {
		return Decision{}, errors.New("idempotency is unavailable")
	}
	owner := uuid.NewString()
	value, err := beginScript.Run(ctx, m.client, []string{"idempotency:" + key}, fingerprint, m.cfg.ProcessingTTL.Milliseconds(), owner).Slice()
	if err != nil {
		return Decision{}, fmt.Errorf("begin idempotency request: %w", err)
	}
	if len(value) != 3 {
		return Decision{}, errors.New("invalid idempotency state response")
	}
	decision := Decision{State: State(fmt.Sprint(value[0]))}
	if decision.State == StateAcquired {
		decision.Owner = owner
	}
	if response := fmt.Sprint(value[1]); response != "" {
		decision.Response = json.RawMessage(response)
	}
	if failure := fmt.Sprint(value[2]); failure != "" {
		if err := json.Unmarshal([]byte(failure), &decision.Failure); err != nil {
			return Decision{}, fmt.Errorf("decode idempotency failure: %w", err)
		}
	}
	return decision, nil
}

var finishScript = redis.NewScript(`
if redis.call('HGET', KEYS[1], 'state') ~= 'processing' or redis.call('HGET', KEYS[1], 'owner') ~= ARGV[1] then return 0 end
redis.call('HSET', KEYS[1], 'state', ARGV[2], ARGV[3], ARGV[4])
redis.call('HDEL', KEYS[1], 'owner')
redis.call('PEXPIRE', KEYS[1], ARGV[5])
return 1
`)

func (m *Manager) Complete(ctx context.Context, key, owner string, response any) error {
	encoded, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode idempotency response: %w", err)
	}
	changed, err := finishScript.Run(ctx, m.client, []string{"idempotency:" + key}, owner, string(StateCompleted), "response", encoded, m.cfg.ResultTTL.Milliseconds()).Int()
	if err != nil {
		return fmt.Errorf("complete idempotency request: %w", err)
	}
	if changed != 1 {
		return errors.New("idempotency ownership expired")
	}
	return nil
}

func (m *Manager) Fail(ctx context.Context, key, owner string, failure Failure) error {
	encoded, err := json.Marshal(failure)
	if err != nil {
		return fmt.Errorf("encode idempotency failure: %w", err)
	}
	changed, err := finishScript.Run(ctx, m.client, []string{"idempotency:" + key}, owner, string(StateFailed), "failure", encoded, m.cfg.FailureTTL.Milliseconds()).Int()
	if err != nil {
		return fmt.Errorf("fail idempotency request: %w", err)
	}
	if changed != 1 {
		return errors.New("idempotency ownership expired")
	}
	return nil
}
