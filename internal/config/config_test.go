package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfig_AuthorizationRequiresConfiguredUpstream(t *testing.T) {
	cfg, err := Load("../../config/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Authorization.Enabled = true
	delete(cfg.Outbound.GRPC, "authorization")
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "outbound.grpc.authorization") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestConfig_DatabaseRequiresApplicationUpstream(t *testing.T) {
	cfg, err := Load("../../config/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database.Enabled = true
	cfg.Database.Type = "postgres"
	cfg.Database.DSN = "postgres://audit"
	delete(cfg.Outbound.GRPC, "application")
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "outbound.grpc.application") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestConfig_OutboundPSKRequiresTLSOrExplicitDevelopmentOptIn(t *testing.T) {
	cfg, err := Load("../../config/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	application := cfg.Outbound.GRPC["application"]
	application.Auth = ClientAuth{Type: "psk", Token: strings.Repeat("p", 32)}
	cfg.Outbound.GRPC["application"] = application
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "TLS or explicit allow_insecure") {
		t.Fatalf("Validate() error = %v", err)
	}
	application.TLS.AllowInsecure = true
	cfg.Outbound.GRPC["application"] = application
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with development opt-in error = %v", err)
	}
	if err := validateClientPolicy("application", application.Auth, application.Retry, application.Breaker, application.TLS, true); err == nil || !strings.Contains(err.Error(), "production") {
		t.Fatalf("production validateClientPolicy() error = %v", err)
	}
}

func TestProductionProfileAuthenticatesApplicationGrantChecks(t *testing.T) {
	content, err := os.ReadFile("../../config/config-production.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `auth: {type: psk, token: ""}`) {
		t.Fatal("production application upstream does not require an injected PSK")
	}
}

func TestConfig_ProductionRequiresAuthorization(t *testing.T) {
	cfg, err := Load("../../config/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.App.Env = "production"
	cfg.GRPC.Enabled = false
	cfg.GRPC.ReflectionEnabled = false
	cfg.Swagger.RequireAuth = true
	cfg.Auth.JWKSURL = "https://identity.example.test/.well-known/jwks.json"
	cfg.Authorization.Enabled = false
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "authorization must be enabled") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestLoad_EnvironmentOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("http:\n  address: 127.0.0.1:8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_HTTP_ADDRESS", "127.0.0.1:9090")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTP.Address != "127.0.0.1:9090" {
		t.Fatalf("HTTP.Address = %q, want %q", cfg.HTTP.Address, "127.0.0.1:9090")
	}
}

func TestConfig_ValidateJWTSecret(t *testing.T) {
	t.Parallel()
	cfg := Config{HTTP: HTTP{Address: "127.0.0.1:8080"}, Auth: Auth{ClientID: "client", ClientSecret: "secret"}, JWT: JWT{Secret: "short"}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}

func TestLoadWithProfile_MergesProfileThenEnvironment(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "config.yaml")
	profile := filepath.Join(dir, "config-test.yaml")
	if err := os.WriteFile(base, []byte("app:\n  env: development\nlog:\n  level: info\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profile, []byte("log:\n  level: debug\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_LOG_LEVEL", "error")
	cfg, err := LoadWithProfile(base, "test")
	if err != nil {
		t.Fatalf("LoadWithProfile() error = %v", err)
	}
	if cfg.App.Env != "test" || cfg.Runtime.ActiveProfile != "test" {
		t.Fatalf("active profile = %q/%q", cfg.App.Env, cfg.Runtime.ActiveProfile)
	}
	if cfg.Log.Level != "error" {
		t.Fatalf("Log.Level = %q, want environment override", cfg.Log.Level)
	}
	if len(cfg.Runtime.ConfigFiles) != 2 || cfg.Runtime.ConfigFiles[1] != profile {
		t.Fatalf("ConfigFiles = %v", cfg.Runtime.ConfigFiles)
	}
}

func TestConfig_ValidateAuthSkipPattern(t *testing.T) {
	t.Parallel()
	cfg := Config{HTTP: HTTP{Address: "127.0.0.1:8080", RequestTimeout: time.Second}, Health: Health{DatabaseTimeout: time.Second, RedisTimeout: time.Second}, User: User{CacheTTL: time.Second, LockTTL: time.Second, LockRetryDelay: time.Millisecond}, Auth: Auth{SkipHTTPPaths: []string{"/api/v1/[broken"}}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid wildcard error")
	}
}

func TestConfig_ValidateAutoMigration(t *testing.T) {
	t.Parallel()
	cfg := Config{
		HTTP:      HTTP{Address: "127.0.0.1:8080", RequestTimeout: time.Second},
		Health:    Health{DatabaseTimeout: time.Second, RedisTimeout: time.Second},
		User:      User{CacheTTL: time.Second, LockTTL: time.Second, LockRetryDelay: time.Millisecond},
		Migration: Migration{AutoUp: true, Path: "migrations/postgres"},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want auto migration dependency error")
	}
}
func TestLoad_UsesCanonicalPlatformEventStream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("http:\n  address: 127.0.0.1:8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EventBus.StreamName != "PLATFORM_EVENTS" || len(cfg.EventBus.Subjects) != 1 || cfg.EventBus.Subjects[0] != "platform.>" {
		t.Fatalf("unexpected event stream defaults: %q %#v", cfg.EventBus.StreamName, cfg.EventBus.Subjects)
	}
}
