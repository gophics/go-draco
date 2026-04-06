GOCACHE ?= $(abspath .gocache)
GOLANGCI_LINT_CACHE ?= $(abspath .golangci-cache)
export GOCACHE
export GOLANGCI_LINT_CACHE

.PHONY: test vet race lint deadcode bench release-check

test:
	go test ./... -count=1

vet:
	go vet ./...

race:
	go test ./... -race -count=1

lint:
	golangci-lint run ./...

deadcode:
	deadcode -test ./...

bench:
	go test -run '^$$' -bench . -benchmem ./...

release-check: test vet race lint
