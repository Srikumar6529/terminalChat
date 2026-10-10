package anthropic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStress_SuccessStreaming(t *testing.T) {
	ts := mockStreamServer(sampleSSEStream(3))
	defer ts.Close()
	c := NewClientWithHTTPClient("k", ts.Client())
	c.BaseURL = ts.URL
	for i := 0; i < 50; i++ {
		text, err := c.Stream(context.Background(), Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
		if err != nil || text != "xxx" {
			t.Fatalf("i=%d text=%q err=%v", i, text, err)
		}
	}
}

func TestStress_HTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"400", 400, `{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`},
		{"401", 401, `{"type":"error","error":{"type":"authentication_error","message":"nope"}}`},
		{"429", 429, `{"type":"error","error":{"type":"rate_limit_error","message":"slow"}}`},
		{"500", 500, `{"type":"error","error":{"type":"api_error","message":"boom"}}`},
		{"503", 503, `{"type":"error","error":{"type":"api_error","message":"down"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer ts.Close()
			c := NewClientWithHTTPClient("k", ts.Client())
			c.BaseURL = ts.URL
			_, err := c.Stream(context.Background(), Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != tc.status {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestStress_IncompleteAndMidstreamError(t *testing.T) {
	t.Run("eof_before_stop", func(t *testing.T) {
		body := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"p\"}}\n\n"
		_, err := readSSE(strings.NewReader(body), nil)
		if !errors.Is(err, ErrIncompleteStream) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("midstream_error", func(t *testing.T) {
		body := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"p\"}}\n\n" +
			"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"api_error\",\"message\":\"mid\"}}\n\n"
		text, err := readSSE(strings.NewReader(body), nil)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || text != "p" {
			t.Fatalf("text=%q err=%v", text, err)
		}
	})
}

func TestStress_CancelBeforeAndDuringStream(t *testing.T) {
	t.Run("before_headers", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}))
		defer ts.Close()
		c := NewClientWithHTTPClient("k", NewHTTPClient(TransportConfig{ResponseHeaderTimeout: 2 * time.Second}))
		c.BaseURL = ts.URL
		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			_, err := c.Stream(ctx, Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
			errCh <- err
		}()
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("server did not see request")
		}
		cancel()
		select {
		case err := <-errCh:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err=%v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("canceled Stream did not return")
		}
		close(release)
	})
	t.Run("during_stream", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"p\"}}\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}))
		defer ts.Close()
		c := NewClientWithHTTPClient("k", ts.Client())
		c.BaseURL = ts.URL
		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			_, err := c.Stream(ctx, Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
			errCh <- err
		}()
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("server did not start stream")
		}
		cancel()
		select {
		case err := <-errCh:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err=%v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("canceled Stream did not return")
		}
		close(release)
	})
}

func TestStress_CallbackFailure(t *testing.T) {
	ts := mockStreamServer(sampleSSEStream(3))
	defer ts.Close()
	c := NewClientWithHTTPClient("k", ts.Client())
	c.BaseURL = ts.URL
	want := errors.New("cb")
	_, err := c.Stream(context.Background(), Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, func(string) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
}

func TestStress_ReusableClientNoGoroutineGrowth(t *testing.T) {
	ts := mockStreamServer(sampleSSEStream(2))
	defer ts.Close()
	c := NewClientWithHTTPClient("k", NewHTTPClient(TransportConfig{}))
	c.BaseURL = ts.URL
	req := Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}
	for i := 0; i < 20; i++ {
		if _, err := c.Stream(context.Background(), req, nil); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	base := runtime.NumGoroutine()
	for i := 0; i < 100; i++ {
		if _, err := c.Stream(context.Background(), req, nil); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > base+20 {
		t.Fatalf("goroutine growth: base=%d after=%d", base, after)
	}
}

func TestStress_ClosedLoopConcurrency(t *testing.T) {
	var hits atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sampleSSEStream(2))
	}))
	defer ts.Close()

	for _, workers := range []int{1, 5, 10, 25} {
		t.Run("n="+strconv.Itoa(workers), func(t *testing.T) {
			hits.Store(0)
			const total = 50
			var wg sync.WaitGroup
			errCh := make(chan error, total)
			jobs := make(chan int, total)
			for i := 0; i < total; i++ {
				jobs <- i
			}
			close(jobs)
			start := time.Now()
			for w := 0; w < workers; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					c := NewClientWithHTTPClient("k", NewHTTPClient(TransportConfig{}))
					c.BaseURL = ts.URL
					for range jobs {
						_, err := c.Stream(context.Background(), Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
						errCh <- err
					}
				}()
			}
			wg.Wait()
			close(errCh)
			elapsed := time.Since(start)
			var fail int
			for err := range errCh {
				if err != nil {
					fail++
				}
			}
			if fail != 0 || hits.Load() != total {
				t.Fatalf("fail=%d hits=%d", fail, hits.Load())
			}
			t.Logf("closed-loop workers=%d total=%d elapsed=%v rps≈%.1f", workers, total, elapsed, float64(total)/elapsed.Seconds())
		})
	}
}
