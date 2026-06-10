ROOT_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
include .make/vars.mk
include .make/help.mk
include .make/deps.mk
include .make/openapi.mk
include .make/go.mk
