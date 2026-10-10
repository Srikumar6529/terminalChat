.PHONY: check fmt vet test race

# Development hygiene checks for Stage 7+.
check: fmt vet test race

fmt:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:" && gofmt -l . && exit 1)

vet:
	go vet ./...

test:
	go test ./...

race:
	go test -race ./...
