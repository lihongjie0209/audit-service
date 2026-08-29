package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/lihongjie0209/audit-service/internal/config"
)

func TestNew_DependencyGraph(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		App:  config.App{Name: "test", Env: "test", ShutdownTimeout: time.Second},
		HTTP: config.HTTP{Address: "127.0.0.1:0", ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second, MaxBodyBytes: 1024},
		Log:  config.Log{Level: "error", Format: "json", File: filepath.Join(t.TempDir(), "app.log"), MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1},
		JWT:  config.JWT{Issuer: "test", TTL: time.Hour},
		Cron: config.Cron{Timezone: "UTC"},
	}
	application := New(cfg)
	if err := application.Err(); err != nil {
		t.Fatalf("New() dependency graph error = %v", err)
	}
}
