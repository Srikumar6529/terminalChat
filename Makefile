.PHONY: check fmt vet test race

# Development hygiene checks (also used by CI).
check: fmt vet test race

fmt:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:" && gofmt -l . && exit 1)

vet:
	go vet ./...

test:
	go test -count=1 ./...

race:
	go test -race -count=1 ./...
