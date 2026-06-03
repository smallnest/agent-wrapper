.PHONY: all build test fmt lint vet clean

BINARY := agent-wrapper
GO := go
GOFMT := gofumpt
LINT := golangci-lint

all: fmt lint test build

build:
	$(GO) build -o bin/$(BINARY) ./cmd/agent-wrapper

test:
	$(GO) test -race -count=1 ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

lint:
	$(LINT) run ./...

clean:
	rm -rf bin/
