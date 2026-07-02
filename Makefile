.DEFAULT_GOAL := help

.PHONY: help lint test

help: ## Show this help
	@grep -E '^[a-z][a-zA-Z0-9_-]*:.*## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*## "}; {printf "  %-10s %s\n", $$1, $$2}'

lint: ## Run golangci-lint
	golangci-lint run ./...

test: ## Run tests
	go test ./...
