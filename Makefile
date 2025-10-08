# All targets are placeholders with echo statements to keep CI green until implemented.

SHELL := /bin/bash

.PHONY: tidy test clean build run clean-build


tidy:
	go mod tidy

test:
	go test ./... -v

clean:
	go clean -modcache
	go clean -testcache
	go clean -cache
	go clean -moddir
	go clean -modcache
	go clean -testcache
	go clean -cache
	go clean -moddir

build:
	go build -o ttt ./cmd/server/main.go

run:
	./ttt

clean-build:
	go clean -modcache
	go clean -testcache
	go clean -cache
	go clean -moddir
	go build -o ttt ./cmd/server/main.go