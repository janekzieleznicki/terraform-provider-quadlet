# Makefile for terraform-provider-quadlet
#
# Targets:
#   help            — self-documenting (default)
#   build           — compile the provider binary
#   install         — install the provider binary into GOBIN
#   fmt             — format Go and Terraform files
#   lint            — run golangci-lint
#   test            — tier 1 hermetic tests (no TF_ACC)
#   testacc         — tier 2 acceptance tests against Terraform
#   testacc-tofu    — tier 2 acceptance tests against OpenTofu
#   testacc-container — tier 2 acceptance tests via container harness
#   docs            — regenerate provider documentation
#   sweep           — sweep test the provider
#   clean           — remove built binary and test cache

GO ?= go
BINARY ?= terraform-provider-quadlet
MODULE_PATH := github.com/janekzieleznicki/terraform-provider-quadlet

.PHONY: help build install fmt lint test testacc testacc-tofu testacc-container docs sweep clean

help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build the provider binary
	$(GO) build -v -o $(BINARY) .

install: ## Install the provider binary into GOBIN (use with a dev_overrides block)
	$(GO) install -v .

fmt: ## Format Go sources and Terraform examples
	$(GO) fmt ./...
	@if command -v terraform >/dev/null 2>&1; then \
		terraform fmt examples/; \
	else \
		echo "terraform not found; skipping terraform fmt"; \
	fi

lint: ## Run golangci-lint
	golangci-lint run

test: ## Tier 1 hermetic tests (no TF_ACC)
	$(GO) test ./... -timeout 120s

testacc: ## Tier 2 acceptance tests against Terraform
	TF_ACC=1 $(GO) test ./internal/provider/ -v -cover -timeout 30m

testacc-tofu: ## Tier 2 acceptance tests against OpenTofu
	@if ! command -v tofu >/dev/null 2>&1; then \
		echo "tofu not found on PATH; install OpenTofu or set TF_ACC_TERRAFORM_PATH"; \
		exit 1; \
	fi
	TF_ACC=1 \
		TF_ACC_TERRAFORM_PATH=$(shell command -v tofu) \
		TF_ACC_PROVIDER_HOST=registry.opentofu.org \
		TF_ACC_PROVIDER_NAMESPACE=janekzieleznicki \
		$(GO) test ./internal/provider/ -v -cover -timeout 30m

testacc-container: ## Tier 2 acceptance tests via container harness
	@if [ -x ./test/container/run-acc.sh ]; then \
		./test/container/run-acc.sh; \
	else \
		echo "./test/container/run-acc.sh not found — Phase 4 deliverable; skipping."; \
	fi

docs: ## Regenerate provider documentation
	$(GO) tool tfplugindocs generate

sweep: ## Sweep test the provider
	TF_ACC=1 $(GO) test ./internal/provider/ -v -sweep=all -timeout 30m

clean: ## Remove built binary and Go test cache artifacts
	rm -f $(BINARY)
	$(GO) clean -testcache
