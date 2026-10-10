package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func mockStreamServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, body)
	}))
}

func BenchmarkStream_MockSmall(b *testing.B) {
	body := sampleSSEStream(20)
	ts := mockStreamServer(body)
	defer ts.Close()

	c := NewClientWithHTTPClient("bench-key", NewHTTPClient(TransportConfig{
		ResponseHeaderTimeout: 5 * time.Second,
	}))
	c.BaseURL = ts.URL

	req := Request{Model: "mock", MaxTokens: 64, Messages: []Message{{Role: "user", Content: "hi"}}}
	want := strings.Repeat("x", 20)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		text, err := c.Stream(context.Background(), req, nil)
		if err != nil || text != want {
			b.Fatalf("text=%q err=%v", text, err)
		}
	}
}

func BenchmarkStream_WarmReuse(b *testing.B) {
	body := sampleSSEStream(10)
	ts := mockStreamServer(body)
	defer ts.Close()

	req := Request{Model: "mock", MaxTokens: 64, Messages: []Message{{Role: "user", Content: "hi"}}}
	c := NewClientWithHTTPClient("bench-key", NewHTTPClient(TransportConfig{}))
	c.BaseURL = ts.URL
	if _, err := c.Stream(context.Background(), req, nil); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Stream(context.Background(), req, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// Cold clients open a new TCP conn per call; keep this a short test so loopback
// TIME_WAIT cannot exhaust ephemeral ports under go test -bench.
func TestStream_ColdClientSmoke(t *testing.T) {
	ts := mockStreamServer(sampleSSEStream(5))
	defer ts.Close()
	req := Request{Model: "mock", MaxTokens: 32, Messages: []Message{{Role: "user", Content: "hi"}}}
	for i := 0; i < 25; i++ {
		c := NewClientWithHTTPClient("bench-key", NewHTTPClient(TransportConfig{}))
		c.BaseURL = ts.URL
		if _, err := c.Stream(context.Background(), req, nil); err != nil {
			t.Fatalf("i=%d: %v", i, err)
		}
	}
}

func TestStream_TTFTMock(t *testing.T) {
	body := sampleSSEStream(5)
	ts := mockStreamServer(body)
	defer ts.Close()

	c := NewClientWithHTTPClient("bench-key", ts.Client())
	c.BaseURL = ts.URL

	var first time.Duration
	start := time.Now()
	_, err := c.Stream(context.Background(), Request{
		Model: "mock", MaxTokens: 16, Messages: []Message{{Role: "user", Content: "hi"}},
	}, func(string) error {
		if first == 0 {
			first = time.Since(start)
		}
		return nil
	})
	total := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if first == 0 || first > total {
		t.Fatalf("first=%v total=%v", first, total)
	}
	t.Logf("mock TTFT=%v total=%v (plain HTTP httptest; not model latency)", first, total)
}
