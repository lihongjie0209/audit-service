package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-redsync/redsync/v4"
	redsyncgoredis "github.com/go-redsync/redsync/v4/redis/goredis/v9"
	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/redis/go-redis/v9"
)

func Open(ctx context.Context, cfg config.Redis) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{Addr: cfg.Address, Username: cfg.Username, Password: cfg.Password, DB: cfg.DB, DialTimeout: cfg.DialTimeout, ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return client, nil
}

const lockKeyPrefix = "lock:"

type Locker struct{ redsync *redsync.Redsync }
type Lock struct{ mutex *redsync.Mutex }

func NewLocker(client redis.UniversalClient) *Locker {
	return &Locker{redsync: redsync.New(redsyncgoredis.NewPool(client))}
}

func (l *Locker) TryLock(ctx context.Context, key string, ttl time.Duration) (*Lock, bool, error) {
	mutex, err := l.newMutex(key, ttl, redsync.WithTries(1))
	if err != nil {
		return nil, false, err
	}
	if err := mutex.TryLockContext(ctx); err != nil {
		if isContention(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("try redis lock %q: %w", key, err)
	}
	return &Lock{mutex: mutex}, true, nil
}

func (l *Locker) Lock(ctx context.Context, key string, ttl, retryDelay time.Duration) (*Lock, error) {
	if retryDelay <= 0 {
		return nil, errors.New("lock retry delay must be positive")
	}
	mutex, err := l.newMutex(key, ttl, redsync.WithRetryDelay(retryDelay))
	if err != nil {
		return nil, err
	}
	if err := mutex.LockContext(ctx); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("wait for redis lock %q: %w", key, ctxErr)
		}
		return nil, fmt.Errorf("acquire redis lock %q: %w", key, err)
	}
	return &Lock{mutex: mutex}, nil
}

func (l *Locker) newMutex(key string, ttl time.Duration, options ...redsync.Option) (*redsync.Mutex, error) {
	if key == "" {
		return nil, errors.New("lock key must not be empty")
	}
	if ttl <= 0 {
		return nil, errors.New("lock ttl must be positive")
	}
	options = append(options, redsync.WithExpiry(ttl))
	return l.redsync.NewMutex(lockKeyPrefix+key, options...), nil
}

func (l *Lock) Extend(ctx context.Context) error {
	ok, err := l.mutex.ExtendContext(ctx)
	if err != nil {
		return fmt.Errorf("extend redis lock %q: %w", l.mutex.Name(), err)
	}
	if !ok {
		return fmt.Errorf("extend redis lock %q: lock is no longer owned", l.mutex.Name())
	}
	return nil
}

func (l *Lock) Unlock(ctx context.Context) error {
	ok, err := l.mutex.UnlockContext(ctx)
	if err != nil {
		return fmt.Errorf("release redis lock %q: %w", l.mutex.Name(), err)
	}
	if !ok {
		return fmt.Errorf("release redis lock %q: lock is no longer owned", l.mutex.Name())
	}
	return nil
}

func (l *Lock) Until() time.Time { return l.mutex.Until() }

func isContention(err error) bool {
	if errors.Is(err, redsync.ErrFailed) {
		return true
	}
	var taken *redsync.ErrTaken
	return errors.As(err, &taken)
}
