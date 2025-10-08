# All targets are placeholders with echo statements to keep CI green until implemented.

SHELL := /bin/bash

.PHONY: tidy test clean build run clean-build


tidy:
	echo "Tidying dependencies..."
	go mod tidy

test:
	echo "Running tests..."
	go test ./... -v

clean:
	echo "Cleaning..."
	go clean -modcache
	go clean -testcache
	go clean -cache
	go clean -moddir
	go clean -modcache
	go clean -testcache
	go clean -cache
	go clean -moddir

build:
	echo "Building server..."
	go build -o ttt ./cmd/server/main.go

run:
	echo "Running server... (after building!)"
	./ttt

clean-build:
	echo "Cleaning and building server..."
	go clean -modcache
	go clean -testcache
	go clean -cache
	go clean -moddir
	go build -o ttt ./cmd/server/main.go