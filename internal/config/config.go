package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Runtime       Runtime       `mapstructure:"-"`
	App           App           `mapstructure:"app"`
	HTTP          HTTP          `mapstructure:"http"`
	GRPC          GRPC          `mapstructure:"grpc"`
	Log           Log           `mapstructure:"log"`
	Database      Database      `mapstructure:"database"`
	Redis         Redis         `mapstructure:"redis"`
	Health        Health        `mapstructure:"health"`
	RateLimit     RateLimit     `mapstructure:"rate_limit"`
	Observability Observability `mapstructure:"observability"`
	Swagger       Swagger       `mapstructure:"swagger"`
	JWT           JWT           `mapstructure:"jwt"`
	Auth          Auth          `mapstructure:"auth"`
	Authorization Authorization `mapstructure:"authorization"`
	Cron          Cron          `mapstructure:"cron"`
	Migration     Migration     `mapstructure:"migration"`
	User          User          `mapstructure:"user"`
	Idempotency   Idempotency   `mapstructure:"idempotency"`
	Outbound      Outbound      `mapstructure:"outbound"`
	EventBus      EventBus      `mapstructure:"event_bus"`
}

type Runtime struct {
	ActiveProfile string   `json:"active_profile"`
	ConfigFiles   []string `json:"config_files"`
}

type App struct {
	Name            string        `mapstructure:"name"`
	Schema          string        `mapstructure:"schema"`
	Env             string        `mapstructure:"env"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
}
type HTTP struct {
	Address        string        `mapstructure:"address"`
	ReadTimeout    time.Duration `mapstructure:"read_timeout"`
	WriteTimeout   time.Duration `mapstructure:"write_timeout"`
	IdleTimeout    time.Duration `mapstructure:"idle_timeout"`
	RequestTimeout time.Duration `mapstructure:"request_timeout"`
	MaxBodyBytes   int64         `mapstructure:"max_body_bytes"`
	TrustedProxies []string      `mapstructure:"trusted_proxies"`
	CORS           CORS          `mapstructure:"cors"`
}

type CORS struct {
	Enabled        bool          `mapstructure:"enabled"`
	AllowedOrigins []string      `mapstructure:"allowed_origins"`
	AllowedHeaders []string      `mapstructure:"allowed_headers"`
	ExposedHeaders []string      `mapstructure:"exposed_headers"`
	MaxAge         time.Duration `mapstructure:"max_age"`
}
type GRPC struct {
	Enabled           bool    `mapstructure:"enabled"`
	Address           string  `mapstructure:"address"`
	ReflectionEnabled bool    `mapstructure:"reflection_enabled"`
	MaxReceiveBytes   int     `mapstructure:"max_receive_bytes"`
	TLS               GRPCTLS `mapstructure:"tls"`
}
type GRPCTLS struct {
	Enabled      bool   `mapstructure:"enabled"`
	CertFile     string `mapstructure:"cert_file"`
	KeyFile      string `mapstructure:"key_file"`
	ClientCAFile string `mapstructure:"client_ca_file"`
}
type Log struct {
	Level      string `mapstructure:"level"`
	Format     string `mapstructure:"format"`
	File       string `mapstructure:"file"`
	MaxSizeMB  int    `mapstructure:"max_size_mb"`
	MaxBackups int    `mapstructure:"max_backups"`
	MaxAgeDays int    `mapstructure:"max_age_days"`
	Compress   bool   `mapstructure:"compress"`
}
type Database struct {
	Enabled         bool          `mapstructure:"enabled"`
	Name            string        `mapstructure:"name"`
	Schema          string        `mapstructure:"schema"`
	Type            string        `mapstructure:"type"`
	DSN             string        `mapstructure:"dsn"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`
	PingTimeout     time.Duration `mapstructure:"ping_timeout"`
}
type Redis struct {
	Enabled      bool          `mapstructure:"enabled"`
	Address      string        `mapstructure:"address"`
	Username     string        `mapstructure:"username"`
	Password     string        `mapstructure:"password"`
	DB           int           `mapstructure:"db"`
	DialTimeout  time.Duration `mapstructure:"dial_timeout"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

type Health struct {
	DatabaseTimeout time.Duration `mapstructure:"database_timeout"`
	RedisTimeout    time.Duration `mapstructure:"redis_timeout"`
}
type RateLimit struct {
	Enabled  bool          `mapstructure:"enabled"`
	FailOpen bool          `mapstructure:"fail_open"`
	IP       RateLimitRule `mapstructure:"ip"`
	API      RateLimitRule `mapstructure:"api"`
	User     RateLimitRule `mapstructure:"user"`
	Login    RateLimitRule `mapstructure:"login"`
}
type RateLimitRule struct {
	Rate   int           `mapstructure:"rate"`
	Burst  int           `mapstructure:"burst"`
	Period time.Duration `mapstructure:"period"`
}
type Observability struct {
	MetricsEnabled     bool    `mapstructure:"metrics_enabled"`
	TracingEnabled     bool    `mapstructure:"tracing_enabled"`
	TracingEndpoint    string  `mapstructure:"tracing_endpoint"`
	TracingSampleRatio float64 `mapstructure:"tracing_sample_ratio"`
	PprofEnabled       bool    `mapstructure:"pprof_enabled"`
	PprofToken         string  `mapstructure:"pprof_token"`
}
type Swagger struct {
	Enabled     bool `mapstructure:"enabled"`
	RequireAuth bool `mapstructure:"require_auth"`
}
type JWT struct {
	Issuer string        `mapstructure:"issuer"`
	Secret string        `mapstructure:"secret"`
	TTL    time.Duration `mapstructure:"ttl"`
}
type Auth struct {
	JWKSURL         string   `mapstructure:"jwks_url"`
	Issuer          string   `mapstructure:"issuer"`
	Audience        string   `mapstructure:"audience"`
	ClientID        string   `mapstructure:"client_id"`
	ClientSecret    string   `mapstructure:"client_secret"`
	SkipHTTPPaths   []string `mapstructure:"skip_http_paths"`
	SkipGRPCMethods []string `mapstructure:"skip_grpc_methods"`
	PSK             PSK      `mapstructure:"psk"`
}
type PSK struct {
	Enabled     bool     `mapstructure:"enabled"`
	Key         string   `mapstructure:"key"`
	HTTPPaths   []string `mapstructure:"http_paths"`
	GRPCMethods []string `mapstructure:"grpc_methods"`
}
type Authorization struct {
	Enabled bool `mapstructure:"enabled"`
}
type Cron struct {
	Enabled    bool   `mapstructure:"enabled"`
	Timezone   string `mapstructure:"timezone"`
	SampleSpec string `mapstructure:"sample_spec"`
}
type Migration struct {
	AutoUp       bool   `mapstructure:"auto_up"`
	CreateSchema bool   `mapstructure:"create_schema"`
	Path         string `mapstructure:"path"`
	DatabaseURL  string `mapstructure:"database_url"`
	Table        string `mapstructure:"table"`
	Schema       string `mapstructure:"-"`
	DatabaseName string `mapstructure:"-"`
}
type User struct {
	CacheTTL       time.Duration `mapstructure:"cache_ttl"`
	LockTTL        time.Duration `mapstructure:"lock_ttl"`
	LockRetryDelay time.Duration `mapstructure:"lock_retry_delay"`
}
type Idempotency struct {
	Enabled       bool          `mapstructure:"enabled"`
	ProcessingTTL time.Duration `mapstructure:"processing_ttl"`
	ResultTTL     time.Duration `mapstructure:"result_ttl"`
	FailureTTL    time.Duration `mapstructure:"failure_ttl"`
}
type EventBus struct {
	Enabled            bool          `mapstructure:"enabled"`
	URLs               []string      `mapstructure:"urls"`
	StreamName         string        `mapstructure:"stream_name"`
	Subjects           []string      `mapstructure:"subjects"`
	Storage            string        `mapstructure:"storage"`
	MaxAge             time.Duration `mapstructure:"max_age"`
	DuplicateWindow    time.Duration `mapstructure:"duplicate_window"`
	ConnectTimeout     time.Duration `mapstructure:"connect_timeout"`
	ReconnectWait      time.Duration `mapstructure:"reconnect_wait"`
	PublishTimeout     time.Duration `mapstructure:"publish_timeout"`
	ConsumerAckWait    time.Duration `mapstructure:"consumer_ack_wait"`
	ConsumerMaxDeliver int           `mapstructure:"consumer_max_deliver"`
}
type Outbound struct {
	HTTP map[string]HTTPUpstream `mapstructure:"http"`
	GRPC map[string]GRPCUpstream `mapstructure:"grpc"`
}
type HTTPUpstream struct {
	BaseURL string        `mapstructure:"base_url"`
	Timeout time.Duration `mapstructure:"timeout"`
	Auth    ClientAuth    `mapstructure:"auth"`
	Retry   Retry         `mapstructure:"retry"`
	Breaker Breaker       `mapstructure:"breaker"`
	TLS     ClientTLS     `mapstructure:"tls"`
}
type GRPCUpstream struct {
	Target  string        `mapstructure:"target"`
	Timeout time.Duration `mapstructure:"timeout"`
	Auth    ClientAuth    `mapstructure:"auth"`
	Retry   Retry         `mapstructure:"retry"`
	Breaker Breaker       `mapstructure:"breaker"`
	TLS     ClientTLS     `mapstructure:"tls"`
}
type ClientAuth struct {
	Type  string `mapstructure:"type"`
	Token string `mapstructure:"token"`
}
type Retry struct {
	MaxAttempts    int           `mapstructure:"max_attempts"`
	InitialBackoff time.Duration `mapstructure:"initial_backoff"`
	MaxBackoff     time.Duration `mapstructure:"max_backoff"`
	Methods        []string      `mapstructure:"methods"`
}
type Breaker struct {
	Enabled          bool          `mapstructure:"enabled"`
	FailureThreshold uint32        `mapstructure:"failure_threshold"`
	OpenTimeout      time.Duration `mapstructure:"open_timeout"`
}
type ClientTLS struct {
	Enabled    bool   `mapstructure:"enabled"`
	ServerName string `mapstructure:"server_name"`
	CAFile     string `mapstructure:"ca_file"`
	CertFile   string `mapstructure:"cert_file"`
	KeyFile    string `mapstructure:"key_file"`
}

func Load(path string) (Config, error) { return LoadWithProfile(path, "") }

func LoadWithProfile(path, explicitProfile string) (Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetEnvPrefix("APP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	if err := v.BindEnv("app.env", "APP_ENV", "APP_APP_ENV"); err != nil {
		return Config{}, fmt.Errorf("bind environment profile: %w", err)
	}
	setDefaults(v)
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if path != "" || !errors.As(err, &notFound) {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
	}
	profile := strings.ToLower(strings.TrimSpace(explicitProfile))
	if profile == "" {
		profile = strings.ToLower(strings.TrimSpace(v.GetString("app.env")))
	}
	if !validProfile.MatchString(profile) {
		return Config{}, fmt.Errorf("invalid environment profile %q", profile)
	}
	loadedFiles := []string{path}
	profilePath := profileConfigPath(path, profile)
	if profilePath != path {
		if _, err := os.Stat(profilePath); err == nil {
			v.SetConfigFile(profilePath)
			if err := v.MergeInConfig(); err != nil {
				return Config{}, fmt.Errorf("merge profile config %q: %w", profilePath, err)
			}
			loadedFiles = append(loadedFiles, profilePath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("inspect profile config %q: %w", profilePath, err)
		}
	}
	v.Set("app.env", profile)
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	cfg.Migration.Schema = cfg.Database.Schema
	cfg.Migration.DatabaseName = cfg.Database.Name
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	cfg.Runtime = Runtime{ActiveProfile: profile, ConfigFiles: loadedFiles}
	return cfg, nil
}

var validProfile = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
var validMigrationTable = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

func profileConfigPath(path, profile string) string {
	extension := filepath.Ext(path)
	base := strings.TrimSuffix(path, extension)
	return base + "-" + profile + extension
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("app.name", "audit-service")
	v.SetDefault("app.env", "development")
	v.SetDefault("app.shutdown_timeout", "10s")
	v.SetDefault("http.address", "127.0.0.1:8080")
	v.SetDefault("http.read_timeout", "10s")
	v.SetDefault("http.write_timeout", "15s")
	v.SetDefault("http.idle_timeout", "60s")
	v.SetDefault("http.request_timeout", "10s")
	v.SetDefault("http.max_body_bytes", 1<<20)
	v.SetDefault("http.trusted_proxies", []string{})
	v.SetDefault("http.cors.enabled", false)
	v.SetDefault("http.cors.allowed_origins", []string{})
	v.SetDefault("http.cors.allowed_headers", []string{"Authorization", "Content-Type", "X-Request-ID"})
	v.SetDefault("http.cors.exposed_headers", []string{"X-Request-ID", "X-RateLimit-Limit", "X-RateLimit-Remaining", "Retry-After"})
	v.SetDefault("http.cors.max_age", "12h")
	v.SetDefault("grpc.enabled", true)
	v.SetDefault("grpc.address", "127.0.0.1:9090")
	v.SetDefault("grpc.reflection_enabled", true)
	v.SetDefault("grpc.max_receive_bytes", 16<<20)
	v.SetDefault("grpc.tls.enabled", false)
	v.SetDefault("grpc.tls.cert_file", "")
	v.SetDefault("grpc.tls.key_file", "")
	v.SetDefault("grpc.tls.client_ca_file", "")
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("log.file", "logs/app.log")
	v.SetDefault("log.max_size_mb", 100)
	v.SetDefault("log.max_backups", 10)
	v.SetDefault("log.max_age_days", 30)
	v.SetDefault("log.compress", true)
	v.SetDefault("database.ping_timeout", "5s")
	v.SetDefault("database.max_open_conns", 25)
	v.SetDefault("database.max_idle_conns", 10)
	v.SetDefault("database.conn_max_lifetime", "5m")
	v.SetDefault("database.conn_max_idle_time", "1m")
	v.SetDefault("database.enabled", false)
	v.SetDefault("database.name", "platform")
	v.SetDefault("database.schema", "audit")
	v.SetDefault("database.type", "postgres")
	v.SetDefault("database.dsn", "")
	v.SetDefault("redis.address", "127.0.0.1:6379")
	v.SetDefault("redis.dial_timeout", "5s")
	v.SetDefault("redis.read_timeout", "3s")
	v.SetDefault("redis.write_timeout", "3s")
	v.SetDefault("redis.enabled", false)
	v.SetDefault("redis.username", "")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("health.database_timeout", "2s")
	v.SetDefault("health.redis_timeout", "2s")
	v.SetDefault("rate_limit.enabled", false)
	v.SetDefault("rate_limit.fail_open", false)
	setRateLimitDefaults(v, "rate_limit.ip", 120, 30)
	setRateLimitDefaults(v, "rate_limit.api", 10000, 1000)
	setRateLimitDefaults(v, "rate_limit.user", 600, 100)
	setRateLimitDefaults(v, "rate_limit.login", 10, 3)
	v.SetDefault("observability.metrics_enabled", true)
	v.SetDefault("observability.tracing_enabled", false)
	v.SetDefault("observability.tracing_endpoint", "http://127.0.0.1:4318")
	v.SetDefault("observability.tracing_sample_ratio", 0.1)
	v.SetDefault("observability.pprof_enabled", false)
	v.SetDefault("observability.pprof_token", "")
	v.SetDefault("swagger.enabled", true)
	v.SetDefault("swagger.require_auth", false)
	v.SetDefault("jwt.issuer", "audit-service")
	v.SetDefault("jwt.secret", "")
	v.SetDefault("jwt.ttl", "2h")
	v.SetDefault("auth.client_id", "")
	v.SetDefault("auth.jwks_url", "")
	v.SetDefault("auth.issuer", "identity-service")
	v.SetDefault("auth.audience", "audit-service")
	v.SetDefault("auth.client_secret", "")
	v.SetDefault("auth.skip_http_paths", []string{"/api/v1/version"})
	v.SetDefault("auth.skip_grpc_methods", []string{"/grpc.health.v1.Health/*"})
	v.SetDefault("auth.psk.enabled", false)
	v.SetDefault("auth.psk.key", "")
	v.SetDefault("auth.psk.http_paths", []string{})
	v.SetDefault("auth.psk.grpc_methods", []string{})
	v.SetDefault("authorization.enabled", false)
	v.SetDefault("cron.enabled", true)
	v.SetDefault("cron.timezone", "Asia/Shanghai")
	v.SetDefault("cron.sample_spec", "0 */5 * * * *")
	v.SetDefault("migration.path", "migrations/postgres")
	v.SetDefault("migration.database_url", "")
	v.SetDefault("migration.auto_up", false)
	v.SetDefault("migration.create_schema", false)
	v.SetDefault("migration.table", "audit_schema_migrations")
	v.SetDefault("user.cache_ttl", "5m")
	v.SetDefault("user.lock_ttl", "10s")
	v.SetDefault("user.lock_retry_delay", "100ms")
	v.SetDefault("idempotency.enabled", false)
	v.SetDefault("idempotency.processing_ttl", "30s")
	v.SetDefault("idempotency.result_ttl", "24h")
	v.SetDefault("idempotency.failure_ttl", "5m")
	v.SetDefault("event_bus.enabled", false)
	v.SetDefault("event_bus.urls", []string{"nats://127.0.0.1:4222"})
	v.SetDefault("event_bus.stream_name", "PLATFORM_EVENTS")
	v.SetDefault("event_bus.subjects", []string{"platform.>"})
	v.SetDefault("event_bus.storage", "file")
	v.SetDefault("event_bus.max_age", "168h")
	v.SetDefault("event_bus.duplicate_window", "10m")
	v.SetDefault("event_bus.connect_timeout", "5s")
	v.SetDefault("event_bus.reconnect_wait", "1s")
	v.SetDefault("event_bus.publish_timeout", "5s")
	v.SetDefault("event_bus.consumer_ack_wait", "30s")
	v.SetDefault("event_bus.consumer_max_deliver", 10)
	v.SetDefault("outbound.http", map[string]any{})
	v.SetDefault("outbound.grpc", map[string]any{})
}

func (c Config) Validate() error {
	if !validMigrationTable.MatchString(c.Database.Name) {
		return errors.New("database.name must contain lowercase letters, digits, or underscores and be at most 63 characters")
	}
	if c.Database.Schema != "" && !validMigrationTable.MatchString(c.Database.Schema) {
		return errors.New("database.schema must contain lowercase letters, digits, or underscores and be at most 63 characters")
	}
	if c.HTTP.Address == "" {
		return errors.New("http.address is required")
	}
	if c.GRPC.Enabled && (c.GRPC.Address == "" || c.GRPC.MaxReceiveBytes <= 0) {
		return errors.New("enabled grpc requires address and positive max_receive_bytes")
	}
	if c.GRPC.TLS.Enabled && (c.GRPC.TLS.CertFile == "" || c.GRPC.TLS.KeyFile == "") {
		return errors.New("grpc tls requires cert_file and key_file")
	}
	if c.App.Env == "production" && c.GRPC.Enabled && !c.GRPC.TLS.Enabled {
		return errors.New("grpc tls must be enabled in production")
	}
	if c.App.Env == "production" && c.GRPC.ReflectionEnabled {
		return errors.New("grpc reflection must be disabled in production")
	}
	if c.Database.Enabled && (c.Database.DSN == "" || !isDBType(c.Database.Type)) {
		return errors.New("enabled database requires dsn and type mysql, postgres, or kingbase")
	}
	if c.Migration.AutoUp && (!c.Database.Enabled || c.Migration.Path == "" || c.Migration.DatabaseURL == "" || !validMigrationTable.MatchString(c.Migration.Table)) {
		return errors.New("migration.auto_up requires enabled database, path, database_url, and a valid service-specific table")
	}
	if c.Redis.Enabled && c.Redis.Address == "" {
		return errors.New("enabled redis requires address")
	}
	if c.HTTP.RequestTimeout <= 0 || c.Health.DatabaseTimeout <= 0 || c.Health.RedisTimeout <= 0 {
		return errors.New("http and health timeouts must be positive")
	}
	if c.RateLimit.Enabled && !c.Redis.Enabled {
		return errors.New("rate_limit requires redis.enabled")
	}
	if c.RateLimit.Enabled {
		for name, rule := range map[string]RateLimitRule{"ip": c.RateLimit.IP, "api": c.RateLimit.API, "user": c.RateLimit.User, "login": c.RateLimit.Login} {
			if rule.Rate <= 0 || rule.Burst <= 0 || rule.Period <= 0 {
				return fmt.Errorf("rate_limit.%s values must be positive", name)
			}
		}
	}
	if c.HTTP.CORS.Enabled && len(c.HTTP.CORS.AllowedOrigins) == 0 {
		return errors.New("cors.allowed_origins is required when cors is enabled")
	}
	if c.Observability.TracingSampleRatio < 0 || c.Observability.TracingSampleRatio > 1 {
		return errors.New("observability.tracing_sample_ratio must be between 0 and 1")
	}
	if c.Observability.TracingEnabled && c.Observability.TracingEndpoint == "" {
		return errors.New("observability.tracing_endpoint is required")
	}
	if c.Observability.PprofEnabled && len(c.Observability.PprofToken) < 32 {
		return errors.New("observability.pprof_token must contain at least 32 bytes")
	}
	if c.App.Env == "production" && c.Swagger.Enabled && !c.Swagger.RequireAuth {
		return errors.New("swagger.require_auth must be enabled in production")
	}
	if c.App.Env == "production" && (c.Auth.JWKSURL == "" || c.Auth.Issuer == "" || c.Auth.Audience == "") {
		return errors.New("production auth requires jwks_url, issuer, and audience")
	}
	if c.App.Env == "production" && !c.Authorization.Enabled {
		return errors.New("authorization must be enabled in production")
	}
	if c.Authorization.Enabled {
		if _, ok := c.Outbound.GRPC["authorization"]; !ok {
			return errors.New("enabled authorization requires outbound.grpc.authorization")
		}
	}
	if (c.Auth.ClientID != "" || c.Auth.ClientSecret != "") && len(c.JWT.Secret) < 32 {
		return errors.New("jwt.secret must contain at least 32 bytes when auth is enabled")
	}
	for _, pattern := range c.Auth.SkipHTTPPaths {
		if !strings.HasPrefix(pattern, "/api/") {
			return fmt.Errorf("auth.skip_http_paths contains path outside /api %q", pattern)
		}
		if _, err := path.Match(pattern, "/validation/target"); err != nil {
			return fmt.Errorf("auth.skip_http_paths contains invalid pattern %q: %w", pattern, err)
		}
	}
	for _, method := range c.Auth.SkipGRPCMethods {
		if !strings.HasPrefix(method, "/") || strings.Count(method, "/") != 2 {
			return fmt.Errorf("auth.skip_grpc_methods contains invalid method pattern %q", method)
		}
		if _, err := path.Match(method, "/validation/target"); err != nil {
			return fmt.Errorf("auth.skip_grpc_methods contains invalid pattern %q: %w", method, err)
		}
	}
	if c.Auth.PSK.Enabled && (len(c.Auth.PSK.Key) < 32 || len(c.Auth.PSK.HTTPPaths)+len(c.Auth.PSK.GRPCMethods) == 0) {
		return errors.New("enabled auth.psk requires a key of at least 32 bytes and at least one route pattern")
	}
	for _, pattern := range c.Auth.PSK.HTTPPaths {
		if !strings.HasPrefix(pattern, "/api/") {
			return fmt.Errorf("auth.psk.http_paths contains path outside /api %q", pattern)
		}
		if _, err := path.Match(pattern, "/validation/target"); err != nil {
			return fmt.Errorf("auth.psk.http_paths contains invalid pattern %q: %w", pattern, err)
		}
	}
	for _, pattern := range c.Auth.PSK.GRPCMethods {
		if !strings.HasPrefix(pattern, "/") || strings.Count(pattern, "/") != 2 {
			return fmt.Errorf("auth.psk.grpc_methods contains invalid method pattern %q", pattern)
		}
		if _, err := path.Match(pattern, "/validation/target"); err != nil {
			return fmt.Errorf("auth.psk.grpc_methods contains invalid pattern %q: %w", pattern, err)
		}
	}
	if c.User.CacheTTL <= 0 || c.User.LockTTL <= 0 || c.User.LockRetryDelay <= 0 {
		return errors.New("user cache and lock durations must be positive")
	}
	if c.Idempotency.Enabled && (!c.Redis.Enabled || c.Idempotency.ProcessingTTL <= 0 || c.Idempotency.ResultTTL <= 0 || c.Idempotency.FailureTTL <= 0) {
		return errors.New("enabled idempotency requires redis and positive TTL values")
	}
	if c.EventBus.Enabled && (len(c.EventBus.URLs) == 0 || c.EventBus.StreamName == "" || len(c.EventBus.Subjects) == 0 || (c.EventBus.Storage != "file" && c.EventBus.Storage != "memory") || c.EventBus.MaxAge <= 0 || c.EventBus.DuplicateWindow <= 0 || c.EventBus.ConnectTimeout <= 0 || c.EventBus.ReconnectWait <= 0 || c.EventBus.PublishTimeout <= 0 || c.EventBus.ConsumerAckWait <= 0 || c.EventBus.ConsumerMaxDeliver <= 0) {
		return errors.New("enabled event_bus requires URLs, stream, subjects, valid storage, positive timeouts, and max deliveries")
	}
	for name, upstream := range c.Outbound.HTTP {
		if upstream.BaseURL == "" || upstream.Timeout <= 0 {
			return fmt.Errorf("outbound.http.%s requires base_url and positive timeout", name)
		}
		if err := validateClientPolicy(name, upstream.Auth, upstream.Retry, upstream.Breaker, upstream.TLS); err != nil {
			return err
		}
	}
	for name, upstream := range c.Outbound.GRPC {
		if upstream.Target == "" || upstream.Timeout <= 0 {
			return fmt.Errorf("outbound.grpc.%s requires target and positive timeout", name)
		}
		if err := validateClientPolicy(name, upstream.Auth, upstream.Retry, upstream.Breaker, upstream.TLS); err != nil {
			return err
		}
	}
	return nil
}

func validateClientPolicy(name string, auth ClientAuth, retry Retry, breaker Breaker, tls ClientTLS) error {
	if auth.Type != "" && auth.Type != "bearer" && auth.Type != "psk" {
		return fmt.Errorf("outbound %s auth.type must be bearer or psk", name)
	}
	if auth.Type != "" && auth.Token == "" {
		return fmt.Errorf("outbound %s auth.token is required", name)
	}
	if auth.Type != "" && !tls.Enabled {
		return fmt.Errorf("outbound %s credentials require TLS", name)
	}
	if retry.MaxAttempts < 1 || retry.MaxAttempts > 5 || retry.InitialBackoff <= 0 || retry.MaxBackoff < retry.InitialBackoff {
		return fmt.Errorf("outbound %s retry policy is invalid", name)
	}
	for _, pattern := range retry.Methods {
		if !strings.HasPrefix(pattern, "/") || strings.Count(pattern, "/") != 2 {
			return fmt.Errorf("outbound %s retry method pattern is invalid", name)
		}
		if _, err := path.Match(pattern, "/validation/target"); err != nil {
			return fmt.Errorf("outbound %s retry method pattern is invalid: %w", name, err)
		}
	}
	if breaker.Enabled && (breaker.FailureThreshold == 0 || breaker.OpenTimeout <= 0) {
		return fmt.Errorf("outbound %s breaker policy is invalid", name)
	}
	if tls.Enabled && (tls.CertFile == "") != (tls.KeyFile == "") {
		return fmt.Errorf("outbound %s TLS certificate and key must be configured together", name)
	}
	return nil
}

func setRateLimitDefaults(v *viper.Viper, key string, rate, burst int) {
	v.SetDefault(key+".rate", rate)
	v.SetDefault(key+".burst", burst)
	v.SetDefault(key+".period", "1m")
}

func isDBType(value string) bool {
	return value == "mysql" || value == "postgres" || value == "kingbase"
}
