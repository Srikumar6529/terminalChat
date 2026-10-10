# Benchmark results

Captured on **2026-10-10**, `darwin/arm64`, Apple M4, Go 1.27.0.

**Interpretation rules**

- Mock / `httptest` numbers measure **client + SSE parse + local HTTP**, not model latency.
- Live pilot numbers are **model + network dominated**; a single trial is not a ranking.
- Do **not** claim this CLI is “faster than the SDK” from mock-only data or one live run.
- Root module stays stdlib-only; SDK is only in this `benchmarks/` module (`anthropic-sdk-go@v1.80.0`).

## Phase map

| Phase | Deliverable | Status |
|-------|-------------|--------|
| 0 | Plan / constraints | done |
| 1 | SSE microbenches (`anthropic/sse_bench_test.go`) | done |
| 2 | Mock stream benches + raw-vs-SDK module | done |
| 3 | Optional live pilot (`-tags=live`) | done (1 trial) |
| 4 | Stress / error / cancel / history | done |
| 5 | Closed-loop concurrency (mock) | done |
| 6–8 | README + RESULTS + offline suite | done |

## Root SSE / stream microbenches

`go test ./anthropic -bench='BenchmarkReadSSE|BenchmarkStream' -benchmem -count=5`

Representative medians (5 runs):

| Benchmark | ns/op | B/op | allocs/op |
|-----------|------:|-----:|----------:|
| `BenchmarkReadSSE_Small` (5 deltas) | ~6.5k | ~68k | 39 |
| `BenchmarkReadSSE_Large` (1000 deltas) | ~443k | ~358k | 5022 |
| `BenchmarkReadSSE_Callback` (100 deltas) | ~48k | ~95k | 518 |
| `BenchmarkReadSSE_ChunkedCRLF` (50 deltas, 17-byte reads) | ~29k | ~81k | 267 |
| `BenchmarkStream_MockSmall` (20 deltas over httptest) | ~50k | ~85k | 229 |
| `BenchmarkStream_WarmReuse` (10 deltas, reused client) | ~44k | ~82k | 174 |

Cold-client path is covered by `TestStream_ColdClientSmoke` (fixed 25 iters) to avoid loopback ephemeral-port exhaustion under `-bench`.

## Raw vs SDK mock compare

`cd benchmarks && go test -bench=BenchmarkCompare -benchmem -count=5`

Same SSE fixture (50 text deltas), shared `http.Client` / `httptest` server, `WithBaseURL` for the SDK.

| Client | ns/op | B/op | allocs/op |
|--------|------:|-----:|----------:|
| raw (`terminalchat/anthropic`) | ~67k | ~94k | ~383 |
| SDK `NewStreaming` | ~156k | ~168k | ~1342 |

On this mock fixture the raw client is roughly **2.3× lower ns/op** and **~3.5× fewer allocs**. That is parser/stack overhead only.

Mock TTFT smoke (`TestCompare_MockTTFT`): both first-token times are sub-millisecond on loopback (noise floor).

## Closed-loop concurrency (mock)

`go test -v -run TestConcurrency_ClosedLoopCompare` (40 requests / worker count):

| Workers | raw RPS (approx) | sdk RPS (approx) |
|--------:|-----------------:|-----------------:|
| 1 | ~6.9k | ~5.4k |
| 5 | ~14.9k | ~11.3k |
| 10 | ~16.8k | ~12.4k |

Root stress closed-loop (`TestStress_ClosedLoopConcurrency`, 50 reqs) similarly saturates quickly on httptest (~12k–23k RPS depending on workers).

## Stress / reliability

Root + package tests (all pass under `make check` and `-race`):

- HTTP 400/401/429/500/503 → `*APIError`
- Incomplete stream (`ErrIncompleteStream`) and mid-stream SSE `error`
- Cancel before headers and during body (returns `context.Canceled`)
- Callback failure propagation
- Reused client without unbounded goroutine growth
- History rollback on failed turns (`TestStress_HistoryRollbackAndRecovery`)

## Live pilot (optional)

```bash
cd benchmarks && go test -tags=live -count=1 -run LivePilot -v
```

One trial against `claude-sonnet-4-5`, prompt `Reply with exactly: ok` (key not logged):

| Client | TTFT | total | response bytes |
|--------|-----:|------:|---------------:|
| raw | 1.17s | 1.17s | 2 |
| SDK | 717ms | 780ms | 2 |

Variance is expected; this does **not** establish a client winner. Re-run many times (and/or with benchstat) before drawing product conclusions.

## How to reproduce

See [README.md](README.md). Offline suite:

```bash
make check
make bench-test
make bench
```
