.PHONY: build test bench

build:
	go build -tags=amd64 ./...

test:
	go test -tags=amd64 ./...

bench:
	go test -tags=amd64 -bench=. ./...
