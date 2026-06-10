.PHONY: help

MAKE_INCLUDES := $(wildcard $(dir $(lastword $(MAKEFILE_LIST)))*.mk)

help: ## Show available targets
	@printf '\nUsage: make [target]\n\nTargets:\n'
	@grep -hE '^[a-zA-Z0-9_.-]+:.*##' Makefile $(MAKE_INCLUDES) 2>/dev/null | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' | sort -u
	@printf '\n'

.DEFAULT_GOAL := help
