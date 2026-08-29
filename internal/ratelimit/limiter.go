package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"time"

	redisrate "github.com/go-redis/redis_rate/v10"
	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/redis/go-redis/v9"
)

type Result struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
}
type Limiter struct {
	enabled  bool
	failOpen bool
	backend  *redisrate.Limiter
}

func New(client *redis.Client, cfg config.Config) *Limiter {
	limiter := &Limiter{enabled: cfg.RateLimit.Enabled, failOpen: cfg.RateLimit.FailOpen}
	if client != nil {
		limiter.backend = redisrate.NewLimiter(client)
	}
	return limiter
}
func (l *Limiter) Enabled() bool  { return l.enabled }
func (l *Limiter) FailOpen() bool { return l.failOpen }
func (l *Limiter) Allow(ctx context.Context, key string, rule config.RateLimitRule) (Result, error) {
	if !l.enabled {
		return Result{Allowed: true}, nil
	}
	if l.backend == nil {
		return Result{}, errors.New("redis rate limiter is unavailable")
	}
	result, err := l.backend.Allow(ctx, key, redisrate.Limit{Rate: rule.Rate, Burst: rule.Burst, Period: rule.Period})
	if err != nil {
		return Result{}, fmt.Errorf("check redis rate limit: %w", err)
	}
	return Result{Allowed: result.Allowed > 0, Limit: rule.Burst, Remaining: result.Remaining, RetryAfter: result.RetryAfter}, nil
}
