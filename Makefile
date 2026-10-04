.PHONY: run build test test-race vet fmt lint tidy schema-export docker-up docker-down seed

run:
	go run ./cmd/gateway

build:
	go build -o bin/gateway ./cmd/gateway
	go build -o bin/schema-export ./cmd/schema-export

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

lint: vet
	@test -z "$$(gofmt -l cmd internal)" || (echo "gofmt needed:"; gofmt -l cmd internal; exit 1)

tidy:
	go mod tidy

# Příklad: make schema-export INDEX=documents
INDEX ?= documents
OPENSEARCH_URL ?= http://localhost:9200
schema-export:
	go run ./cmd/schema-export -url $(OPENSEARCH_URL) -index $(INDEX) -out configs/schema_$(INDEX).pl

seed:
	./deploy/seed/seed.sh $(OPENSEARCH_URL)

docker-up:
	docker compose -f deploy/docker-compose.yaml up --build -d

docker-down:
	docker compose -f deploy/docker-compose.yaml down
