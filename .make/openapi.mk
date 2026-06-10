.PHONY: openapi-bundle openapi-validate openapi-split generate generate-check

openapi-bundle: deps-node ## Bundle split OpenAPI spec into a single file
	@mkdir -p $(API_DIR)
	$(NPM_RUN) openapi:bundle

openapi-validate: openapi-bundle ## Validate OpenAPI spec by bundling references
	@test -s $(OPENAPI_BUNDLE)
	@echo "OpenAPI spec is valid: $(OPENAPI_SPEC)"

openapi-split: deps-node ## Split monolithic OpenAPI spec into api/ structure
	$(NPM_RUN) openapi:split

generate: deps-go deps-node openapi-validate ## Generate Go server/types from OpenAPI
	$(OAPI_CODEGEN) -config $(OAPI_CONFIG) $(OPENAPI_SPEC)

generate-check: generate ## Ensure generated Go code is up to date
	@git diff --exit-code -- $(GENERATED_API)
