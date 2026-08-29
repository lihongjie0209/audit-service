package grpcclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/lihongjie0209/audit-service/internal/idempotency"
	"github.com/lihongjie0209/audit-service/internal/observability"
	"github.com/lihongjie0209/audit-service/internal/requestid"
	"github.com/sony/gobreaker/v2"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Config struct {
	Target  string
	Timeout time.Duration
	Token   string
	PSK     string
	TLS     TLSConfig
	Name    string
	Retry   config.Retry
	Breaker config.Breaker
	Metrics *observability.Metrics
}
type TLSConfig struct {
	Enabled            bool
	ServerName         string
	CAFile             string
	CertFile           string
	KeyFile            string
	AllowInsecureToken bool
}

func Dial(cfg Config) (*grpc.ClientConn, error) {
	if cfg.Target == "" {
		return nil, errors.New("grpc client target is required")
	}
	if cfg.Timeout <= 0 {
		return nil, errors.New("grpc client timeout must be positive")
	}
	transport, err := transportCredentials(cfg.TLS)
	if err != nil {
		return nil, err
	}
	if cfg.Token != "" && !cfg.TLS.Enabled && !cfg.TLS.AllowInsecureToken {
		return nil, errors.New("refusing to send grpc bearer token without TLS")
	}
	if cfg.PSK != "" && cfg.Token != "" {
		return nil, errors.New("grpc client bearer token and PSK are mutually exclusive")
	}
	if cfg.PSK != "" && !cfg.TLS.Enabled && !cfg.TLS.AllowInsecureToken {
		return nil, errors.New("refusing to send grpc PSK without TLS")
	}
	interceptors := []grpc.UnaryClientInterceptor{timeoutInterceptor(cfg.Timeout), metadataInterceptor(cfg.Token, cfg.PSK)}
	if cfg.Metrics != nil {
		interceptors = append(interceptors, metricsInterceptor(cfg.Name, cfg.Metrics))
	}
	if cfg.Breaker.Enabled {
		interceptors = append(interceptors, breakerInterceptor(cfg.Name, cfg.Breaker))
	}
	if cfg.Retry.MaxAttempts > 1 {
		interceptors = append(interceptors, retryInterceptor(cfg.Retry))
	}
	options := []grpc.DialOption{grpc.WithTransportCredentials(transport), grpc.WithStatsHandler(otelgrpc.NewClientHandler()), grpc.WithChainUnaryInterceptor(interceptors...)}
	return grpc.NewClient(cfg.Target, options...)
}

func retryInterceptor(cfg config.Retry) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, connection *grpc.ClientConn, invoker grpc.UnaryInvoker, options ...grpc.CallOption) error {
		if !retryGRPCMethod(ctx, method, cfg.Methods) {
			return invoker(ctx, method, req, reply, connection, options...)
		}
		var err error
		for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
			err = invoker(ctx, method, req, reply, connection, options...)
			code := status.Code(err)
			if code != codes.Unavailable && code != codes.ResourceExhausted {
				return err
			}
			if attempt < cfg.MaxAttempts {
				if waitErr := waitRetry(ctx, cfg, attempt); waitErr != nil {
					return waitErr
				}
			}
		}
		return err
	}
}

func retryGRPCMethod(ctx context.Context, method string, patterns []string) bool {
	if _, ok := idempotency.FromContext(ctx); ok {
		return true
	}
	for _, pattern := range patterns {
		if matched, _ := path.Match(pattern, method); matched {
			return true
		}
	}
	return false
}

func waitRetry(ctx context.Context, cfg config.Retry, attempt int) error {
	delay := cfg.InitialBackoff << (attempt - 1)
	if delay > cfg.MaxBackoff {
		delay = cfg.MaxBackoff
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func breakerInterceptor(name string, cfg config.Breaker) grpc.UnaryClientInterceptor {
	breaker := gobreaker.NewCircuitBreaker[any](gobreaker.Settings{
		Name: name, Timeout: cfg.OpenTimeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool { return counts.ConsecutiveFailures >= cfg.FailureThreshold },
		IsExcluded: func(err error) bool {
			code := status.Code(err)
			return code != codes.Unavailable && code != codes.ResourceExhausted && code != codes.DeadlineExceeded
		},
	})
	return func(ctx context.Context, method string, req, reply any, connection *grpc.ClientConn, invoker grpc.UnaryInvoker, options ...grpc.CallOption) error {
		_, err := breaker.Execute(func() (any, error) { return nil, invoker(ctx, method, req, reply, connection, options...) })
		return err
	}
}

func metricsInterceptor(name string, metrics *observability.Metrics) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, connection *grpc.ClientConn, invoker grpc.UnaryInvoker, options ...grpc.CallOption) error {
		started := time.Now()
		err := invoker(ctx, method, req, reply, connection, options...)
		if metrics.Enabled() {
			metrics.OutboundRequests.WithLabelValues("grpc", name, status.Code(err).String()).Inc()
			metrics.OutboundDuration.WithLabelValues("grpc", name).Observe(time.Since(started).Seconds())
		}
		return err
	}
}

func timeoutInterceptor(timeout time.Duration) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, connection *grpc.ClientConn, invoker grpc.UnaryInvoker, options ...grpc.CallOption) error {
		if _, ok := ctx.Deadline(); ok {
			return invoker(ctx, method, req, reply, connection, options...)
		}
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return invoker(callCtx, method, req, reply, connection, options...)
	}
}
func metadataInterceptor(token, psk string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, connection *grpc.ClientConn, invoker grpc.UnaryInvoker, options ...grpc.CallOption) error {
		pairs := []string{}
		if token != "" {
			pairs = append(pairs, "authorization", "Bearer "+token)
		} else if psk != "" {
			pairs = append(pairs, "authorization", "PSK "+psk)
		}
		if requestID, ok := RequestIDFromContext(ctx); ok {
			pairs = append(pairs, "x-request-id", requestID)
		}
		if key, ok := idempotency.FromContext(ctx); ok {
			pairs = append(pairs, "idempotency-key", key)
		}
		if len(pairs) > 0 {
			ctx = metadata.AppendToOutgoingContext(ctx, pairs...)
		}
		return invoker(ctx, method, req, reply, connection, options...)
	}
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return requestid.WithContext(ctx, id)
}
func RequestIDFromContext(ctx context.Context) (string, bool) {
	return requestid.FromContext(ctx)
}
func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	return idempotency.WithContext(ctx, key)
}

func transportCredentials(cfg TLSConfig) (credentials.TransportCredentials, error) {
	if !cfg.Enabled {
		return insecure.NewCredentials(), nil
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: cfg.ServerName}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read grpc CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("parse grpc CA")
		}
		tlsConfig.RootCAs = pool
	}
	if cfg.CertFile != "" || cfg.KeyFile != "" {
		if cfg.CertFile == "" || cfg.KeyFile == "" {
			return nil, errors.New("grpc client certificate and key must be configured together")
		}
		certificate, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load grpc client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	return credentials.NewTLS(tlsConfig), nil
}
