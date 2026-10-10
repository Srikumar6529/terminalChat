package benchmarks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	raw "terminalchat/anthropic"
)

func streamRaw(c *raw.Client) (string, error) {
	return c.Stream(context.Background(), raw.Request{
		Model:     "mock",
		MaxTokens: 64,
		Messages:  []raw.Message{{Role: "user", Content: "hi"}},
	}, nil)
}

func streamSDK(client anthropic.Client) (string, error) {
	stream := client.Messages.NewStreaming(context.Background(), anthropic.MessageNewParams{
		Model:     "mock",
		MaxTokens: 64,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	})
	var text strings.Builder
	for stream.Next() {
		ev := stream.Current()
		if ev.Type == "content_block_delta" && ev.Delta.Type == "text_delta" {
			text.WriteString(ev.Delta.Text)
		}
	}
	if err := stream.Err(); err != nil {
		_ = stream.Close()
		return text.String(), err
	}
	return text.String(), stream.Close()
}

func BenchmarkCompare_MockStream(b *testing.B) {
	const deltas = 50
	body := sampleSSEStream(deltas)
	want := strings.Repeat("x", deltas)
	ts := mockStreamServer(body)
	defer ts.Close()

	httpClient := raw.NewHTTPClient(raw.TransportConfig{})
	rawClient := raw.NewClientWithHTTPClient("bench-key", httpClient)
	rawClient.BaseURL = ts.URL

	sdkClient := anthropic.NewClient(
		option.WithAPIKey("bench-key"),
		option.WithBaseURL(ts.URL),
		option.WithHTTPClient(httpClient),
	)

	b.Run("raw", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			text, err := streamRaw(rawClient)
			if err != nil || text != want {
				b.Fatalf("text=%q err=%v", text, err)
			}
		}
	})

	b.Run("sdk", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			text, err := streamSDK(sdkClient)
			if err != nil || text != want {
				b.Fatalf("text=%q err=%v", text, err)
			}
		}
	})
}

func TestCompare_MockParity(t *testing.T) {
	const deltas = 20
	body := sampleSSEStream(deltas)
	want := strings.Repeat("x", deltas)
	ts := mockStreamServer(body)
	defer ts.Close()

	httpClient := raw.NewHTTPClient(raw.TransportConfig{})
	rawClient := raw.NewClientWithHTTPClient("bench-key", httpClient)
	rawClient.BaseURL = ts.URL
	sdkClient := anthropic.NewClient(
		option.WithAPIKey("bench-key"),
		option.WithBaseURL(ts.URL),
		option.WithHTTPClient(httpClient),
	)

	rawText, err := streamRaw(rawClient)
	if err != nil {
		t.Fatalf("raw: %v", err)
	}
	sdkText, err := streamSDK(sdkClient)
	if err != nil {
		t.Fatalf("sdk: %v", err)
	}
	if rawText != want || sdkText != want {
		t.Fatalf("raw=%q sdk=%q want=%q", rawText, sdkText, want)
	}
}

func TestCompare_MockTTFT(t *testing.T) {
	ts := mockStreamServer(sampleSSEStream(10))
	defer ts.Close()
	httpClient := raw.NewHTTPClient(raw.TransportConfig{})

	rawClient := raw.NewClientWithHTTPClient("bench-key", httpClient)
	rawClient.BaseURL = ts.URL
	var rawFirst time.Duration
	start := time.Now()
	_, err := rawClient.Stream(context.Background(), raw.Request{
		Model: "mock", MaxTokens: 16, Messages: []raw.Message{{Role: "user", Content: "hi"}},
	}, func(string) error {
		if rawFirst == 0 {
			rawFirst = time.Since(start)
		}
		return nil
	})
	rawTotal := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}

	sdkClient := anthropic.NewClient(
		option.WithAPIKey("bench-key"),
		option.WithBaseURL(ts.URL),
		option.WithHTTPClient(httpClient),
	)
	var sdkFirst time.Duration
	start = time.Now()
	stream := sdkClient.Messages.NewStreaming(context.Background(), anthropic.MessageNewParams{
		Model:     "mock",
		MaxTokens: 16,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	})
	for stream.Next() {
		ev := stream.Current()
		if sdkFirst == 0 && ev.Type == "content_block_delta" && ev.Delta.Type == "text_delta" {
			sdkFirst = time.Since(start)
		}
	}
	sdkTotal := time.Since(start)
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()

	t.Logf("mock TTFT raw=%v sdk=%v | total raw=%v sdk=%v (httptest; not model latency)", rawFirst, sdkFirst, rawTotal, sdkTotal)
	if rawFirst == 0 || sdkFirst == 0 {
		t.Fatal("missing first-token timing")
	}
}
