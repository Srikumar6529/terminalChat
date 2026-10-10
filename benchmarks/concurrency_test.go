package benchmarks

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	raw "terminalchat/anthropic"
)

func TestConcurrency_ClosedLoopCompare(t *testing.T) {
	const total = 40
	ts := mockStreamServer(sampleSSEStream(15))
	defer ts.Close()

	for _, workers := range []int{1, 5, 10} {
		t.Run("workers_"+strconv.Itoa(workers), func(t *testing.T) {
			run := func(name string, fn func() error) {
				t.Helper()
				var fail atomic.Int64
				jobs := make(chan struct{}, total)
				for i := 0; i < total; i++ {
					jobs <- struct{}{}
				}
				close(jobs)
				var wg sync.WaitGroup
				start := time.Now()
				for w := 0; w < workers; w++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for range jobs {
							if err := fn(); err != nil {
								fail.Add(1)
							}
						}
					}()
				}
				wg.Wait()
				elapsed := time.Since(start)
				if fail.Load() != 0 {
					t.Fatalf("%s fail=%d", name, fail.Load())
				}
				t.Logf("%s workers=%d total=%d elapsed=%v rps≈%.1f", name, workers, total, elapsed, float64(total)/elapsed.Seconds())
			}

			httpClient := raw.NewHTTPClient(raw.TransportConfig{})
			rawClient := raw.NewClientWithHTTPClient("bench-key", httpClient)
			rawClient.BaseURL = ts.URL
			run("raw", func() error {
				_, err := streamRaw(rawClient)
				return err
			})

			sdkClient := anthropic.NewClient(
				option.WithAPIKey("bench-key"),
				option.WithBaseURL(ts.URL),
				option.WithHTTPClient(httpClient),
			)
			run("sdk", func() error {
				_, err := streamSDK(sdkClient)
				return err
			})
		})
	}
}
