package anthropic

import (
	"net"
	"net/http"
	"time"
)

// Timeout policy (option A):
//
//   - Dial, TLS handshake, and response-header waits are bounded.
//   - http.Client.Timeout is left at 0 so a long-running SSE body is not cut off
//     after headers arrive. Bound stream lifetime with the request context
//     (Ctrl+C) instead of a global client deadline.
//
// Override defaults by passing TransportConfig to NewHTTPClient, or by setting
// Client.HTTPClient to any *http.Client you construct.

const (
	DefaultDialTimeout           = 10 * time.Second
	DefaultTLSHandshakeTimeout   = 10 * time.Second
	DefaultResponseHeaderTimeout = 60 * time.Second
	DefaultIdleConnTimeout       = 90 * time.Second
	DefaultKeepAlive             = 30 * time.Second
)

// TransportConfig controls connection-level timeouts for NewHTTPClient.
// Zero values mean use the Default* constants above.
type TransportConfig struct {
	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
}

func (c TransportConfig) withDefaults() TransportConfig {
	if c.DialTimeout <= 0 {
		c.DialTimeout = DefaultDialTimeout
	}
	if c.TLSHandshakeTimeout <= 0 {
		c.TLSHandshakeTimeout = DefaultTLSHandshakeTimeout
	}
	if c.ResponseHeaderTimeout <= 0 {
		c.ResponseHeaderTimeout = DefaultResponseHeaderTimeout
	}
	return c
}

// NewHTTPClient returns a reusable *http.Client with connection/header timeouts
// and no overall request Timeout (see package comment on timeout policy).
func NewHTTPClient(cfg TransportConfig) *http.Client {
	cfg = cfg.withDefaults()
	dialer := &net.Dialer{
		Timeout:   cfg.DialTimeout,
		KeepAlive: DefaultKeepAlive,
	}
	return &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       DefaultIdleConnTimeout,
			TLSHandshakeTimeout:   cfg.TLSHandshakeTimeout,
			ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}
