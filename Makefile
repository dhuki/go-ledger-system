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
