# Opencord dev helpers.
# Requires modern Go (>= 1.22) and Node (>= 20) on PATH.
SHELL := /bin/bash

.PHONY: help up down logs dev-db dev-server dev-web test build tidy fmt

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

up: ## Build + run the full stack (db + server + web) → http://localhost:3000
	docker compose up --build

down: ## Stop the stack
	docker compose down

logs: ## Tail stack logs
	docker compose logs -f

dev-db: ## Run just Postgres for local development
	docker compose up -d db

dev-server: ## Run the Go server locally (needs dev-db running)
	go run ./cmd/server

dev-web: ## Run the Vite dev server with hot reload (http://localhost:5173)
	cd web && npm install && npm run dev

test: ## Run Go tests
	go test ./...

qa-browser: ## Browser QA — boot a dev stack, drive the real UI (Playwright), tear down
	bash qa/run.sh

build: ## Compile the server binary into ./bin
	go build -o bin/opencord ./cmd/server

tidy: ## Tidy Go modules
	go mod tidy

fmt: ## Format Go code
	go fmt ./...
