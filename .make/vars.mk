SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
MAKEFLAGS += --warn-undefined-variables

API_DIR := $(ROOT_DIR)/api
OPENAPI_SPEC := $(API_DIR)/openapi.yaml
OPENAPI_BUNDLE := $(API_DIR)/openapi.bundled.yaml
OAPI_CONFIG := $(ROOT_DIR)/oapi-codegen.yaml
GENERATED_API := $(ROOT_DIR)/internal/api/api.gen.go

GO := go
GO_MODULE := core-backend
GOBIN ?= $(shell $(GO) env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell $(GO) env GOPATH)/bin
endif

NODE := npm
NPM_CI := $(NODE) ci
NPM_RUN := $(NODE) run

OAPI_CODEGEN_VERSION ?= v2.7.1
OAPI_CODEGEN := $(GO) run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION)

export PATH := $(GOBIN):$(PATH)

.PHONY: all
