package anthropic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResponseHeaderTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer ts.Close()

	c := NewClientWithHTTPClient("test-key", NewHTTPClient(TransportConfig{
		DialTimeout:           time.Second,
		TLSHandshakeTimeout:   time.Second,
		ResponseHeaderTimeout: 50 * time.Millisecond,
	}))
	c.BaseURL = ts.URL

	_, err := c.Stream(context.Background(), Request{
		Model:    "x",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, nil)
	if err == nil {
		t.Fatal("expected header timeout error")
	}
	if !strings.Contains(err.Error(), "Timeout") && !strings.Contains(err.Error(), "timeout") && !errors.Is(err, context.DeadlineExceeded) {
		// net/http wraps this as a url.Error with Timeout() == true
		var netErr interface{ Timeout() bool }
		if !errors.As(err, &netErr) || !netErr.Timeout() {
			t.Fatalf("err = %v, want timeout", err)
		}
	}
}

func TestCancelThenSuccessfulRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			close(started)
			<-release
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n")
		io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer ts.Close()

	httpClient := NewHTTPClient(TransportConfig{
		ResponseHeaderTimeout: 5 * time.Second,
	})
	c := NewClientWithHTTPClient("test-key", httpClient)
	c.BaseURL = ts.URL

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := c.Stream(ctx, Request{Model: "x", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
		errCh <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not see first request")
	}
	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected cancel error")
		}
		if !errors.Is(err, context.Canceled) && !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("err = %v, want canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled Stream did not return")
	}
	close(release)

	text, err := c.Stream(context.Background(), Request{Model: "x", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != "ok" {
		t.Fatalf("text = %q", text)
	}
	if calls < 2 {
		t.Fatalf("calls = %d, want >= 2", calls)
	}
}

func TestNewHTTPClientHasNoOverallTimeout(t *testing.T) {
	c := NewHTTPClient(TransportConfig{})
	if c.Timeout != 0 {
		t.Fatalf("Timeout = %v, want 0 so long SSE bodies are not cut off", c.Timeout)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type %T", c.Transport)
	}
	if tr.ResponseHeaderTimeout != DefaultResponseHeaderTimeout {
		t.Fatalf("ResponseHeaderTimeout = %v", tr.ResponseHeaderTimeout)
	}
	if tr.TLSHandshakeTimeout != DefaultTLSHandshakeTimeout {
		t.Fatalf("TLSHandshakeTimeout = %v", tr.TLSHandshakeTimeout)
	}
}

func TestNewClientReusesHTTPClient(t *testing.T) {
	c := NewClient("k")
	if c.HTTPClient == nil {
		t.Fatal("HTTPClient is nil")
	}
	if c.httpClient() != c.HTTPClient {
		t.Fatal("httpClient() should return the reused instance")
	}
}
