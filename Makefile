SHELL := /bin/bash
export PATH := $(HOME)/.local/go/bin:$(HOME)/go/bin:$(PATH)

MODULES := $(shell find myaccount tir -mindepth 1 -maxdepth 1 -type d 2>/dev/null | sort) $(shell find example -mindepth 1 -maxdepth 1 -type d 2>/dev/null)

.PHONY: all
all: fmt vet lint build

.PHONY: generate
generate:
	./scripts/generate.sh

.PHONY: fmt
fmt:
	@for m in $(MODULES); do gofmt -l -w $$m; done

.PHONY: fmt-check
fmt-check:
	@fail=0; \
	for m in $(MODULES); do \
		diff=$$(gofmt -l $$m); \
		if [ -n "$$diff" ]; then echo "gofmt needed in $$m:"; echo "$$diff"; fail=1; fi; \
	done; \
	exit $$fail

.PHONY: vet
vet:
	@for m in $(MODULES); do (cd $$m && go vet ./...) || exit 1; done

.PHONY: lint
lint:
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "golangci-lint not found, installing..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	fi
	@for m in $(MODULES); do (cd $$m && golangci-lint run -c $(CURDIR)/.golangci.yml ./...) || exit 1; done

.PHONY: build
build:
	@for m in $(MODULES); do (cd $$m && go build ./...) || exit 1; done

.PHONY: tidy
tidy:
	@for m in $(MODULES); do (cd $$m && go mod tidy) || exit 1; done

.PHONY: list-modules
list-modules:
	@for m in $(MODULES); do echo $$m; done

.PHONY: hooks
hooks:
	git config core.hooksPath scripts/hooks
	@echo "commit-msg hook installed (Conventional Commits enforced locally)"
