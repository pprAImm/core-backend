.PHONY: deps deps-go deps-node

deps: deps-go deps-node ## Install Go and Node dependencies

deps-go: ## Download Go modules
	$(GO) mod download

deps-node: ## Install Node dependencies
	$(NPM_CI)
