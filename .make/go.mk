.PHONY: build test fmt vet tidy lint ci

build: deps-go ## Build all Go packages
	$(GO) build ./...

test: deps-go ## Run Go tests
	$(GO) test ./...

fmt: ## Format Go source files
	@gofmt -w $$(find . -name '*.go' -not -path './node_modules/*')

vet: deps-go ## Run go vet
	$(GO) vet ./...

tidy: ## Sync go.mod and go.sum
	$(GO) mod tidy

lint: vet ## Run static checks (go vet; extend with golangci-lint later)
	@echo "Lint passed"

ci: deps openapi-validate generate-check build test ## Run full local/CI pipeline
	@echo "CI checks passed"
