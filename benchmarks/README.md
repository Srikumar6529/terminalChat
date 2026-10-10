# Benchmarks (separate module)

Compares this repo’s stdlib Anthropic client (`terminalchat/anthropic`) with
[`anthropic-sdk-go`](https://github.com/anthropics/anthropic-sdk-go) **v1.80.0**.

The root CLI stays **stdlib-only**. This module pulls the SDK via `go.mod` +
`replace terminalchat => ../`.

## What is measured

| Suite | Meaning |
|-------|---------|
| Mock stream compare | Same `httptest` SSE body; raw `Stream` vs SDK `NewStreaming` |
| Concurrency | Closed-loop RPS with 1/5/10 workers (mock) |
| Root package benches | SSE parser + mock `Stream` in `../anthropic` |
| Stress tests | Errors, cancel, history rollback, goroutine reuse (root + here) |
| Live pilot (`-tags=live`) | Optional real API; **model/network dominated** |

Mock numbers are **client/parser/HTTP stack** cost, not model latency. Do **not**
claim “faster than the SDK” from mock-only data or a single live trial.

## Commands

```bash
# From repo root — product tests (no SDK)
make check

# Benchmarks module — mock parity + concurrency logs
cd benchmarks
go test -count=1 ./...
go test -bench=BenchmarkCompare -benchmem -count=5 ./...
# Closed-loop concurrency timings are logged by TestConcurrency_ClosedLoopCompare (-v)

# Root SSE/stream microbenches
cd ..
go test ./anthropic -bench='BenchmarkReadSSE|BenchmarkStream' -benchmem -count=5

# Optional live pilot (uses ANTHROPIC_API_KEY; never prints the key)
set -a && source ../.env && set +a   # if you keep a local .env
cd benchmarks
go test -tags=live -count=1 -run LivePilot -v
```

Optional: install [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) and compare saved outputs.

## Results

See [RESULTS.md](RESULTS.md) for captured offline numbers from this machine.
