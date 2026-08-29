package outbound

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/lihongjie0209/audit-service/internal/idempotency"
	"github.com/lihongjie0209/audit-service/internal/observability"
	"github.com/sony/gobreaker/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type HTTPClient struct {
	name    string
	baseURL *url.URL
	client  *http.Client
	cfg     config.HTTPUpstream
	breaker *gobreaker.CircuitBreaker[*http.Response]
	metrics *observability.Metrics
}

func (c *HTTPClient) CloseIdleConnections() { c.client.CloseIdleConnections() }

func NewHTTPClient(name string, cfg config.HTTPUpstream, metrics *observability.Metrics) (*HTTPClient, error) {
	if cfg.Auth.Type != "" && !cfg.TLS.Enabled {
		return nil, errors.New("refusing to send outbound HTTP credentials without TLS")
	}
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("parse outbound HTTP base URL %q", cfg.BaseURL)
	}
	transport, err := httpTransport(cfg.TLS, cfg.Timeout)
	if err != nil {
		return nil, err
	}
	client := &HTTPClient{name: name, baseURL: baseURL, cfg: cfg, metrics: metrics, client: &http.Client{Transport: otelhttp.NewTransport(transport), Timeout: cfg.Timeout}}
	if cfg.Breaker.Enabled {
		client.breaker = gobreaker.NewCircuitBreaker[*http.Response](gobreaker.Settings{ //nolint:bodyclose // The caller owns every returned response body.
			Name: name, Timeout: cfg.Breaker.OpenTimeout,
			ReadyToTrip: func(counts gobreaker.Counts) bool { return counts.ConsecutiveFailures >= cfg.Breaker.FailureThreshold },
			IsExcluded:  func(err error) bool { return errors.Is(err, context.Canceled) },
		})
	}
	return client, nil
}

func (c *HTTPClient) Do(ctx context.Context, method, requestPath string, body []byte, headers http.Header) (*http.Response, error) {
	started := time.Now()
	response, err := c.execute(ctx, method, requestPath, body, headers)
	status := "error"
	if response != nil {
		status = strconv.Itoa(response.StatusCode)
	}
	if c.metrics != nil && c.metrics.Enabled() {
		c.metrics.OutboundRequests.WithLabelValues("http", c.name, status).Inc()
		c.metrics.OutboundDuration.WithLabelValues("http", c.name).Observe(time.Since(started).Seconds())
	}
	return response, err
}

func (c *HTTPClient) execute(ctx context.Context, method, requestPath string, body []byte, headers http.Header) (*http.Response, error) {
	if c.breaker == nil {
		return c.doWithRetry(ctx, method, requestPath, body, headers)
	}
	call := func() (*http.Response, error) {
		response, err := c.doWithRetry(ctx, method, requestPath, body, headers)
		if err == nil && response != nil && retryableHTTPStatus(response.StatusCode) {
			return nil, &statusError{response: response}
		}
		return response, err
	}
	response, err := c.breaker.Execute(call)
	var statusErr *statusError
	if errors.As(err, &statusErr) {
		return statusErr.response, nil
	}
	return response, err
}

func (c *HTTPClient) doWithRetry(ctx context.Context, method, requestPath string, body []byte, headers http.Header) (*http.Response, error) {
	reference, err := url.Parse(requestPath)
	if err != nil || reference.IsAbs() || reference.Host != "" {
		return nil, errors.New("outbound HTTP path must be relative")
	}
	target := c.baseURL.ResolveReference(reference).String()
	maxAttempts := 1
	if retryableMethod(ctx, method) {
		maxAttempts = c.cfg.Retry.MaxAttempts
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create outbound HTTP request: %w", err)
		}
		request.Header = headers.Clone()
		if request.Header == nil {
			request.Header = make(http.Header)
		}
		if len(body) > 0 && request.Header.Get("Content-Type") == "" {
			request.Header.Set("Content-Type", "application/json")
		}
		applyHTTPAuth(request, c.cfg.Auth)
		response, err := c.client.Do(request)
		if err == nil && (response == nil || !retryableHTTPStatus(response.StatusCode) || attempt == maxAttempts) {
			return response, nil
		}
		if err != nil && attempt == maxAttempts {
			return nil, fmt.Errorf("call outbound HTTP service: %w", err)
		}
		if response != nil {
			_, _ = io.CopyN(io.Discard, response.Body, 32<<10)
			_ = response.Body.Close()
		}
		if attempt < maxAttempts {
			if err := waitBackoff(ctx, c.cfg.Retry, attempt); err != nil {
				return nil, err
			}
		}
	}
	return nil, errors.New("outbound HTTP attempts exhausted")
}

func retryableMethod(ctx context.Context, method string) bool {
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions || method == http.MethodPut || method == http.MethodDelete {
		return true
	}
	_, ok := idempotency.FromContext(ctx)
	return ok
}
func retryableHTTPStatus(status int) bool {
	return status == 429 || status == 502 || status == 503 || status == 504
}
func applyHTTPAuth(request *http.Request, auth config.ClientAuth) {
	switch auth.Type {
	case "bearer":
		request.Header.Set("Authorization", "Bearer "+auth.Token)
	case "psk":
		request.Header.Set("Authorization", "PSK "+auth.Token)
	}
}
func waitBackoff(ctx context.Context, retry config.Retry, attempt int) error {
	delay := time.Duration(float64(retry.InitialBackoff) * math.Pow(2, float64(attempt-1)))
	if delay > retry.MaxBackoff {
		delay = retry.MaxBackoff
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

type statusError struct{ response *http.Response }

func (e *statusError) Error() string { return "retryable upstream HTTP status " + e.response.Status }

func httpTransport(cfg config.ClientTLS, timeout time.Duration) (*http.Transport, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: cfg.ServerName}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read outbound HTTP CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("parse outbound HTTP CA")
		}
		tlsConfig.RootCAs = pool
	}
	if cfg.CertFile != "" {
		certificate, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load outbound HTTP client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	return &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: tlsConfig, ForceAttemptHTTP2: true, MaxIdleConns: 100, MaxIdleConnsPerHost: 10, IdleConnTimeout: 90 * time.Second, ResponseHeaderTimeout: timeout}, nil
}
