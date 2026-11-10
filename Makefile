SHELL := /bin/bash
.DEFAULT_GOAL := help

COMPOSE          := docker compose --env-file .env -f infra/compose.yaml
PNPM             := corepack pnpm
API_DIR          := apps/api
GOLANGCI         := $(API_DIR)/bin/golangci-lint
GOLANGCI_VERSION := v2.15.0
SQLC             := $(API_DIR)/bin/sqlc
SQLC_VERSION     := v1.31.1
# Loads .env into the recipe's environment (for commands that run on the host).
LOAD_ENV         := set -a && . ./.env && set +a

.PHONY: help setup up down logs ps dev web api migrate-up migrate-down migrate-new gen \
        test test-unit test-go test-go-unit test-web lint lint-go lint-web fmt build

help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

# --- Setup -------------------------------------------------------------------

setup: .env $(GOLANGCI) $(SQLC) ## Create .env, install web deps, download Go modules and tools
	$(PNPM) install --frozen-lockfile
	cd $(API_DIR) && go mod download

.env:
	cp .env.example .env
	@echo "Created .env from .env.example"

# Built with the toolchain pinned in go.mod: golangci-lint refuses newer Go targets.
$(GOLANGCI): $(API_DIR)/go.mod
	cd $(API_DIR) && GOTOOLCHAIN=$$(go mod edit -json | sed -n 's/.*"Toolchain": "\(.*\)".*/\1/p') \
		GOBIN=$$PWD/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	@touch $@

$(SQLC): $(API_DIR)/go.mod
	cd $(API_DIR) && GOBIN=$$PWD/bin go install github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)
	@touch $@

# --- Run ---------------------------------------------------------------------

up: .env ## Start PostgreSQL, run migrations and start the API (Docker Compose)
	$(COMPOSE) up -d --build --wait db api

down: ## Stop the Compose stack (data volume is kept)
	$(COMPOSE) down

logs: ## Follow Compose logs
	$(COMPOSE) logs -f

ps: ## Show Compose services
	$(COMPOSE) ps -a

dev: up web ## Start the stack and the web dev server (http://localhost:3000)

web: .env ## Run the Next.js dev server on the host (proxies /api and /health to the API)
	@$(LOAD_ENV) && curl -fsS -m 2 -o /dev/null "$${API_URL:-http://localhost:8090}/health" \
		|| echo "AVISO: a API não respondeu em $${API_URL:-http://localhost:8090}/health. Rode 'make up' (ou use 'make dev')."
	$(LOAD_ENV) && $(PNPM) --filter web dev

api: .env ## Run the API on the host instead of Compose (stop the Compose api first)
	$(LOAD_ENV) && cd $(API_DIR) && go run ./cmd/casvuno serve

migrate-up: .env ## Apply pending migrations
	$(COMPOSE) run --rm --build migrate migrate up

migrate-down: .env ## Revert the last migration (N=… to revert more)
	$(COMPOSE) run --rm --build migrate migrate down $(or $(N),1)

migrate-new: ## Create an empty up/down migration pair: make migrate-new name=add_things
	infra/scripts/new-migration.sh $(API_DIR)/db/migrations "$(name)"

# --- Code generation ---------------------------------------------------------

gen: $(SQLC) ## Regenerate sqlc code and the typed web API client (commit the result)
	cd $(API_DIR) && bin/sqlc generate
	$(PNPM) --filter web gen:api

# --- Quality -----------------------------------------------------------------

test: test-go test-web ## All tests (Go integration tests need Docker)

test-unit: test-go-unit test-web ## Tests that do not need Docker

test-go: ## Go unit + integration tests (Testcontainers PostgreSQL)
	cd $(API_DIR) && go test -race -count=1 -tags integration ./...

test-go-unit:
	cd $(API_DIR) && go test -race -count=1 ./...

test-web:
	$(PNPM) --filter web test

lint: lint-go lint-web ## Lint and format checks (Go + web)

lint-go: $(GOLANGCI)
	cd $(API_DIR) && bin/golangci-lint run ./... && bin/golangci-lint fmt --diff ./...

lint-web:
	$(PNPM) --filter web lint
	$(PNPM) --filter web typecheck

fmt: $(GOLANGCI) ## Format Go and web code
	cd $(API_DIR) && bin/golangci-lint fmt ./...
	$(PNPM) --filter web format

build: ## Build the API binary, the API image and the static web export
	cd $(API_DIR) && go build -o bin/casvuno ./cmd/casvuno
	$(COMPOSE) build api
	$(PNPM) --filter web build
