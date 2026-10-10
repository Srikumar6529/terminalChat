.PHONY: check fmt vet test race bench-test bench

# Development hygiene checks (also used by CI). Root module is stdlib-only.
check: fmt vet test race

fmt:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:" && gofmt -l . && exit 1)

vet:
	go vet ./...

test:
	go test -count=1 ./...

race:
	go test -race -count=1 ./...

# Separate benchmarks module (pulls anthropic-sdk-go). Mock only by default.
bench-test:
	cd benchmarks && go test -count=1 ./...

bench:
	go test ./anthropic -bench='BenchmarkReadSSE|BenchmarkStream' -benchmem -count=3
	cd benchmarks && go test -bench=BenchmarkCompare -benchmem -count=3 ./...
