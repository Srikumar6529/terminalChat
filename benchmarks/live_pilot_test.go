//go:build live

package benchmarks

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	raw "terminalchat/anthropic"
)

// Optional live pilot (not run by default CI).
//
//	cd benchmarks && go test -tags=live -count=1 -run LivePilot -v
//
// Requires ANTHROPIC_API_KEY. Never logs the key. Measures client-side TTFT/total
// against the real API — dominated by model/network latency, not parser speed.
func TestLivePilot_RawVsSDK(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY unset")
	}
	model := strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL"))
	if model == "" {
		model = "claude-sonnet-4-5"
	}

	prompt := "Reply with exactly: ok"
	httpClient := raw.NewHTTPClient(raw.TransportConfig{
		ResponseHeaderTimeout: 60 * time.Second,
	})

	rawClient := raw.NewClientWithHTTPClient(key, httpClient)
	var rawFirst time.Duration
	start := time.Now()
	rawText, err := rawClient.Stream(context.Background(), raw.Request{
		Model:     model,
		MaxTokens: 32,
		Messages:  []raw.Message{{Role: "user", Content: prompt}},
	}, func(string) error {
		if rawFirst == 0 {
			rawFirst = time.Since(start)
		}
		return nil
	})
	rawTotal := time.Since(start)
	if err != nil {
		t.Fatalf("raw stream: %v", err)
	}

	sdkClient := anthropic.NewClient(
		option.WithAPIKey(key),
		option.WithHTTPClient(httpClient),
	)
	var sdkFirst time.Duration
	start = time.Now()
	stream := sdkClient.Messages.NewStreaming(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 32,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))},
	})
	var sdkText strings.Builder
	for stream.Next() {
		ev := stream.Current()
		if ev.Type == "content_block_delta" && ev.Delta.Type == "text_delta" {
			if sdkFirst == 0 {
				sdkFirst = time.Since(start)
			}
			sdkText.WriteString(ev.Delta.Text)
		}
	}
	sdkTotal := time.Since(start)
	if err := stream.Err(); err != nil {
		t.Fatalf("sdk stream: %v", err)
	}
	_ = stream.Close()

	t.Logf("live model=%s", model)
	t.Logf("raw TTFT=%v total=%v bytes=%d", rawFirst, rawTotal, len(rawText))
	t.Logf("sdk TTFT=%v total=%v bytes=%d", sdkFirst, sdkTotal, sdkText.Len())
	t.Logf("note: live timings are model+network dominated; do not claim client X is faster without many runs")
	if rawFirst == 0 || sdkFirst == 0 || len(rawText) == 0 || sdkText.Len() == 0 {
		t.Fatal("empty live response or missing TTFT")
	}
}
