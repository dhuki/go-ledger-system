.PHONY: up down restart build test

COMPOSE = docker-compose

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

restart:
	$(COMPOSE) down
	$(COMPOSE) up -d --build

build:
	go build -o ./bin/go-ledger-system ./cmd/server/main.go

test:
	go test ./...

test-integration:
	DOCKER_PSQL_DSN="host=localhost port=15432 dbname=postgres user=xxx password=xxx sslmode=disable" \
  		go test -v -count=1 -timeout=60s ./scripts/integration/...
