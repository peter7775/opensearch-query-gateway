.PHONY: run build test tidy

run:
	go run ./cmd/gateway

build:
	go build -o bin/gateway ./cmd/gateway

test:
	go test ./...

tidy:
	go mod tidy
